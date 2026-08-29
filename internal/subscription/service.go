// Package subscription turns a user's provisioned panels into the config their
// client app downloads: a mihomo profile or a base64 list of share links.
package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/sharelink"
	"github.com/PFXDev/FireX/internal/store"
)

// SettingKeyClashTemplate holds the admin-editable mihomo template.
const SettingKeyClashTemplate = "clash.template"

// linkTTL bounds how stale a cached link set may be. Share links only change
// when an admin edits an inbound, so a short cache spares the panels a burst of
// requests when many clients refresh at once.
const linkTTL = 60 * time.Second

// unmatchedSortOrder parks links that could not be traced back to a node after
// every real node in the display order.
const unmatchedSortOrder = 1 << 20

type Service struct {
	db  *store.DB
	mgr *provision.Manager

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	panelID uint
	email   string
}

type cacheEntry struct {
	links   []string
	fetched time.Time
}

func NewService(db *store.DB, mgr *provision.Manager) *Service {
	return &Service{db: db, mgr: mgr, cache: map[cacheKey]cacheEntry{}}
}

// Entry is one client-visible proxy: the node it came from, the share link the
// panel produced, and the mihomo rendering of it.
type Entry struct {
	Node  model.Node
	Name  string
	Link  string
	Clash *clash.Ordered
}

type Result struct {
	User    *model.User
	Entries []Entry
	// Warnings collects per-panel failures and unparsable links; the
	// subscription is still served with whatever did work.
	Warnings []string
}

// Build fetches the user's share links from every panel they are provisioned
// on and matches them back to FireX nodes.
func (s *Service) Build(ctx context.Context, u *model.User) (*Result, error) {
	nodes, err := s.mgr.NodesForUser(u)
	if err != nil {
		return nil, err
	}
	result := &Result{User: u}
	if len(nodes) == 0 {
		return result, nil
	}

	byPanel := map[uint][]model.Node{}
	panelOrder := make([]uint, 0, len(nodes))
	for _, n := range nodes {
		if _, seen := byPanel[n.PanelID]; !seen {
			panelOrder = append(panelOrder, n.PanelID)
		}
		byPanel[n.PanelID] = append(byPanel[n.PanelID], n)
	}

	email := provision.EmailFor(u)
	usedNames := map[string]bool{}
	for _, panelID := range panelOrder {
		var p model.Panel
		if err := s.db.First(&p, panelID).Error; err != nil {
			continue
		}
		links, err := s.links(ctx, &p, email)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("panel %s: %v", p.Name, err))
			continue
		}
		entries, warnings := matchPanel(byPanel[panelID], links, usedNames)
		result.Entries = append(result.Entries, entries...)
		for _, w := range warnings {
			result.Warnings = append(result.Warnings, fmt.Sprintf("panel %s: %s", p.Name, w))
		}
	}

	sort.SliceStable(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i].Node, result.Entries[j].Node
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		return a.ID < b.ID
	})
	return result, nil
}

func (s *Service) links(ctx context.Context, p *model.Panel, email string) ([]string, error) {
	key := cacheKey{p.ID, email}
	s.mu.Lock()
	if entry, ok := s.cache[key]; ok && time.Since(entry.fetched) < linkTTL {
		s.mu.Unlock()
		return entry.links, nil
	}
	s.mu.Unlock()

	links, err := s.mgr.ClientFor(p).ClientLinks(ctx, email)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cacheEntry{links: links, fetched: time.Now()}
	s.mu.Unlock()
	return links, nil
}

// InvalidateUser drops cached links for a user across every panel, so an admin
// edit shows up on the next subscription fetch instead of a minute later.
func (s *Service) InvalidateUser(u *model.User) {
	email := provision.EmailFor(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.cache {
		if key.email == email {
			delete(s.cache, key)
		}
	}
}

func (s *Service) InvalidateAll() {
	s.mu.Lock()
	s.cache = map[cacheKey]cacheEntry{}
	s.mu.Unlock()
}

// matchPanel attributes each share link to the node it came from. The panel's
// link list carries no inbound id, so links are matched on the port they
// advertise — unique per inbound in practice — and anything unmatched is kept
// under the remark the panel gave it rather than dropped.
func matchPanel(nodes []model.Node, links []string, usedNames map[string]bool) ([]Entry, []string) {
	proxies, parseErrs := sharelink.ParseMany(links)
	warnings := make([]string, 0, len(parseErrs))
	for _, err := range parseErrs {
		warnings = append(warnings, err.Error())
	}

	byPort := map[int][]model.Node{}
	for _, n := range nodes {
		byPort[n.Port] = append(byPort[n.Port], n)
	}

	entries := make([]Entry, 0, len(proxies))
	for _, parsed := range proxies {
		proxy := parsed.Proxy
		node, matched := pickNode(byPort, proxy)
		name := proxy.Name
		if matched {
			name = node.DisplayName()
			proxy.UDP = node.UDP
		} else {
			// Sort an unattributed link last instead of ahead of every
			// deliberately ordered node.
			node.SortOrder = unmatchedSortOrder
		}
		name = uniqueName(name, usedNames)
		proxy.Name = name

		entry := Entry{Node: node, Name: name, Link: sharelink.Rename(parsed.Raw, name)}
		if rendered, ok := clash.ProxyEntry(proxy); ok {
			entry.Clash = rendered
		} else {
			warnings = append(warnings, fmt.Sprintf("%s: no mihomo equivalent for %s/%s", name, proxy.Type, proxy.Network))
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func pickNode(byPort map[int][]model.Node, proxy *sharelink.Proxy) (model.Node, bool) {
	candidates := byPort[proxy.Port]
	for _, n := range candidates {
		if strings.EqualFold(n.Protocol, proxy.Type) {
			return n, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return model.Node{}, false
}

// uniqueName keeps proxy names distinct; mihomo silently drops duplicates.
func uniqueName(name string, used map[string]bool) string {
	if name == "" {
		name = "node"
	}
	candidate := name
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s #%d", name, i)
	}
	used[candidate] = true
	return candidate
}

// Clash renders the result as a mihomo profile using the stored template.
func (s *Service) Clash(result *Result) (string, error) {
	template := s.db.GetSetting(SettingKeyClashTemplate, clash.DefaultTemplate)
	nodes := make([]clash.Node, 0, len(result.Entries))
	for _, e := range result.Entries {
		nodes = append(nodes, clash.Node{
			Name:   e.Name,
			Region: e.Node.Region,
			Tags:   splitTags(e.Node.Tags),
			Entry:  e.Clash,
		})
	}
	return clash.Render(template, clash.Input{Nodes: nodes})
}

// Base64 renders the result as the newline-joined, base64-encoded share link
// list that v2rayN-style clients expect.
func Base64(result *Result) string {
	links := make([]string, 0, len(result.Entries))
	for _, e := range result.Entries {
		links = append(links, e.Link)
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
}

// UserInfo is the Subscription-Userinfo header value clients read to show
// remaining quota and expiry.
func UserInfo(u *model.User) string {
	parts := []string{
		fmt.Sprintf("upload=%d", u.Upload),
		fmt.Sprintf("download=%d", u.Download),
		fmt.Sprintf("total=%d", u.TrafficLimit),
	}
	if u.ExpiresAt > 0 {
		parts = append(parts, fmt.Sprintf("expire=%d", u.ExpiresAt/1000))
	}
	return strings.Join(parts, "; ")
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
