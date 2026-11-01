package subscription

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/sharelink"
)

const (
	DefaultExternalUpdateInterval = 3600
	MinExternalUpdateInterval     = 60
	maxExternalSubscriptionBytes  = 10 << 20
)

var (
	externalHTTPClient = &http.Client{Timeout: 30 * time.Second}
	ErrRefreshRunning  = errors.New("外部订阅正在刷新")
)

type externalNode struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Link     string `json:"link,omitempty"`
	Config   string `json:"config"`
}

// ExternalNode is the credential-free management view of one cached proxy.
type ExternalNode struct {
	Name         string
	Protocol     string
	Server       string
	Port         int
	HasShareLink bool
}

func ValidateExternalSubscriptionURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("订阅地址必须是有效的 HTTP(S) URL")
	}
	return value, nil
}

// RefreshExternalSubscription replaces the cached nodes only after a complete
// response yields at least one usable proxy. Failures leave the old set intact.
func (s *Service) RefreshExternalSubscription(ctx context.Context, id uint) error {
	s.externalMu.Lock()
	if s.refreshing[id] {
		s.externalMu.Unlock()
		return ErrRefreshRunning
	}
	s.refreshing[id] = true
	s.externalMu.Unlock()
	defer func() {
		s.externalMu.Lock()
		delete(s.refreshing, id)
		s.externalMu.Unlock()
	}()

	var sub model.ExternalSubscription
	if err := s.db.First(&sub, id).Error; err != nil {
		return err
	}
	nodes, warning, refreshErr := fetchExternalSubscription(ctx, sub.URL)
	now := time.Now().UnixMilli()
	if refreshErr != nil {
		err := s.db.Model(&model.ExternalSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"last_check_at": now,
			"last_error":    refreshErr.Error(),
		}).Error
		if err != nil {
			return err
		}
		return refreshErr
	}
	encoded, err := json.Marshal(nodes)
	if err != nil {
		return err
	}
	return s.db.Model(&model.ExternalSubscription{}).Where("id = ?", id).Updates(map[string]any{
		"nodes":          string(encoded),
		"last_check_at":  now,
		"last_update_at": now,
		"last_error":     warning,
	}).Error
}

// RefreshDueExternalSubscriptions refreshes every enabled subscription whose
// own interval has elapsed. now is explicit so scheduling is deterministic in tests.
func (s *Service) RefreshDueExternalSubscriptions(ctx context.Context, now time.Time) error {
	var subs []model.ExternalSubscription
	if err := s.db.Where("enabled = ?", true).Order("id ASC").Find(&subs).Error; err != nil {
		return err
	}
	var errs []error
	for _, sub := range subs {
		interval := sub.UpdateInterval
		if interval < MinExternalUpdateInterval {
			interval = DefaultExternalUpdateInterval
		}
		due := time.UnixMilli(sub.LastCheckAt).Add(time.Duration(interval) * time.Second)
		if sub.LastCheckAt > 0 && due.After(now) {
			continue
		}
		if err := s.RefreshExternalSubscription(ctx, sub.ID); err != nil && !errors.Is(err, ErrRefreshRunning) && !errors.Is(err, context.Canceled) {
			errs = append(errs, fmt.Errorf("%s: %w", sub.Name, err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

// RunExternalSubscriptions checks for due subscriptions at startup and once a
// minute thereafter. Individual refresh errors are persisted on their rows.
func (s *Service) RunExternalSubscriptions(ctx context.Context) {
	run := func() {
		if err := s.RefreshDueExternalSubscriptions(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("firex: external subscriptions: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

func fetchExternalSubscription(ctx context.Context, rawURL string) ([]externalNode, string, error) {
	validated, err := ValidateExternalSubscriptionURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validated, nil)
	if err != nil {
		return nil, "", errors.New("无法创建订阅请求")
	}
	req.Header.Set("User-Agent", "mihomo/v1.18.0")
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain, application/json, */*")
	resp, err := externalHTTPClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, "", fmt.Errorf("获取订阅失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("获取订阅失败: HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxExternalSubscriptionBytes+1))
	if err != nil {
		return nil, "", errors.New("读取订阅内容失败")
	}
	if len(payload) > maxExternalSubscriptionBytes {
		return nil, "", errors.New("订阅内容超过 10 MiB 限制")
	}
	return parseExternalSubscription(payload)
}

func parseExternalSubscription(payload []byte) ([]externalNode, string, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil, "", errors.New("订阅内容为空")
	}
	if proxies, err := clash.ParseProxies(payload); err == nil {
		nodes := make([]externalNode, 0, len(proxies))
		for _, proxy := range proxies {
			encoded, err := yaml.Marshal(proxy.Entry)
			if err != nil {
				return nil, "", errors.New("无法保存 mihomo 节点")
			}
			protocol, _ := proxy.Entry.Get("type")
			nodes = append(nodes, externalNode{
				Name: proxy.Name, Protocol: fmt.Sprint(protocol), Config: string(encoded),
			})
		}
		if len(nodes) == 0 {
			return nil, "", errors.New("订阅中没有节点")
		}
		return nodes, "", nil
	} else if bytes.Contains(payload, []byte("proxies:")) || bytes.Contains(payload, []byte(`"proxies"`)) {
		return nil, "", fmt.Errorf("解析 mihomo 订阅失败: %w", err)
	}

	raw := payload
	if !bytes.Contains(raw, []byte("://")) {
		decoded, ok := decodeExternalBase64(string(raw))
		if !ok {
			return nil, "", errors.New("无法识别订阅格式")
		}
		raw = decoded
	}
	parsed, parseErrs := sharelink.ParseMany(strings.Split(string(raw), "\n"))
	nodes := make([]externalNode, 0, len(parsed))
	unsupported := 0
	for _, item := range parsed {
		rendered, ok := clash.ProxyEntry(item.Proxy)
		if !ok {
			unsupported++
			continue
		}
		encoded, err := yaml.Marshal(rendered)
		if err != nil {
			unsupported++
			continue
		}
		nodes = append(nodes, externalNode{
			Name: item.Proxy.Name, Protocol: item.Proxy.Type, Link: item.Raw, Config: string(encoded),
		})
	}
	if len(nodes) == 0 {
		return nil, "", errors.New("订阅中没有可解析的节点")
	}
	warning := ""
	if skipped := len(parseErrs) + unsupported; skipped > 0 {
		warning = fmt.Sprintf("已跳过 %d 条无法解析或不受支持的节点", skipped)
	}
	return nodes, warning, nil
}

func decodeExternalBase64(raw string) ([]byte, bool) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		default:
			return r
		}
	}, raw)
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(compact)
		if err == nil && bytes.Contains(decoded, []byte("://")) {
			return decoded, true
		}
	}
	return nil, false
}

// ExternalEntries loads cached proxies for the enabled subscriptions linked to
// groups. It never performs network I/O on a user's subscription request.
func (s *Service) ExternalEntries(groups []model.NodeGroup, usedNames map[string]bool) ([]Entry, []string) {
	if len(groups) == 0 {
		return nil, nil
	}
	groupIDs := make([]uint, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	var subs []model.ExternalSubscription
	err := s.db.Where("enabled = ?", true).
		Where("id IN (?)", s.db.Model(&model.NodeGroupExternalSubscription{}).
			Select("subscription_id").Where("group_id IN ?", groupIDs)).
		Order("id ASC").Find(&subs).Error
	if err != nil {
		return nil, []string{err.Error()}
	}
	var entries []Entry
	var warnings []string
	for _, sub := range subs {
		var nodes []externalNode
		if err := json.Unmarshal([]byte(sub.Nodes), &nodes); err != nil {
			warnings = append(warnings, fmt.Sprintf("外部订阅 %s: 缓存无法读取", sub.Name))
			continue
		}
		for _, node := range nodes {
			rendered, err := clash.ParseProxy([]byte(node.Config))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("外部订阅 %s: 节点 %s 的缓存无效", sub.Name, node.Name))
				continue
			}
			name := uniqueName(node.Name, usedNames)
			rendered.Set("name", name)
			link := ""
			if node.Link != "" {
				link = sharelink.Rename(node.Link, name)
			}
			entries = append(entries, Entry{
				ExternalSubscriptionID: sub.ID, ExternalSubscriptionName: sub.Name,
				Protocol: node.Protocol, Name: name, Link: link, Clash: rendered,
			})
		}
	}
	return entries, warnings
}

// ExternalNodeCount reports the cached node count without exposing the cache.
func ExternalNodeCount(sub *model.ExternalSubscription) int {
	var nodes []externalNode
	if json.Unmarshal([]byte(sub.Nodes), &nodes) != nil {
		return 0
	}
	return len(nodes)
}

// ExternalNodes returns a management view with credentials and share links removed.
func ExternalNodes(sub *model.ExternalSubscription) ([]ExternalNode, error) {
	var nodes []externalNode
	if err := json.Unmarshal([]byte(sub.Nodes), &nodes); err != nil {
		return nil, errors.New("外部订阅缓存无法读取")
	}
	out := make([]ExternalNode, 0, len(nodes))
	for _, node := range nodes {
		entry, err := clash.ParseProxy([]byte(node.Config))
		if err != nil {
			return nil, fmt.Errorf("节点 %s 的缓存无效", node.Name)
		}
		view := ExternalNode{Name: node.Name, Protocol: node.Protocol, HasShareLink: node.Link != ""}
		if server, ok := entry.Get("server"); ok {
			view.Server, _ = server.(string)
		}
		if port, ok := entry.Get("port"); ok {
			switch value := port.(type) {
			case int:
				view.Port = value
			case int64:
				view.Port = int(value)
			case float64:
				view.Port = int(value)
			case string:
				view.Port, _ = strconv.Atoi(value)
			}
		}
		out = append(out, view)
	}
	return out, nil
}
