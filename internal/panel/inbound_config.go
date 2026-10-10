package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
)

// EditableConfiguration keeps all panel options, including unknown extensions,
// but excludes identities, observations and accounts owned by the target panel.
func EditableConfiguration(raw string) (map[string]any, error) {
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil || config == nil {
		return nil, fmt.Errorf("入站配置必须是 JSON 对象")
	}
	for _, key := range []string{"id", "userId", "up", "down", "allTime", "clientStats", "lastTrafficResetTime", "nodeId", "originNodeGuid", "fallbackParent"} {
		delete(config, key)
	}
	for _, key := range []string{"settings", "streamSettings", "sniffing"} {
		value := config[key]
		if text, ok := value.(string); ok {
			if err := json.Unmarshal([]byte(text), &value); err != nil {
				return nil, fmt.Errorf("%s 必须是 JSON 对象", key)
			}
		}
		if value == nil {
			value = map[string]any{}
		}
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s 必须是 JSON 对象", key)
		}
		if key == "settings" {
			delete(obj, "clients")
		}
		config[key] = obj
	}
	return config, nil
}

// SameConfiguration compares two stored configurations after normalization,
// so key order and runtime counters never count as a difference.
func SameConfiguration(a, b string) bool {
	left, err := EditableConfiguration(a)
	if err != nil {
		return false
	}
	right, err := EditableConfiguration(b)
	return err == nil && reflect.DeepEqual(left, right)
}

// MismatchedPath returns the first field of want that actual lacks or holds
// differently, or "" when actual contains all of want. Fields a panel adds
// on its own are not differences. Panels derive the tag from listen and port,
// so it is not compared.
func MismatchedPath(want, actual map[string]any) string {
	return mismatchedPath(want, actual, "")
}

func mismatchedPath(want, actual map[string]any, prefix string) string {
	keys := make([]string, 0, len(want))
	for key := range want {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		} else if key == "tag" {
			continue
		}
		got, exists := actual[key]
		if !exists {
			return path
		}
		w, wok := want[key].(map[string]any)
		a, aok := got.(map[string]any)
		if wok && aok {
			if p := mismatchedPath(w, a, path); p != "" {
				return p
			}
			continue
		}
		if !reflect.DeepEqual(want[key], got) {
			return path
		}
	}
	return ""
}

// ValidateTemplate checks the shared part of a template. Node fields are
// validated once WithNode has filled them in.
func ValidateTemplate(config map[string]any) error {
	if protocol, _ := config["protocol"].(string); protocol == "" {
		return fmt.Errorf("protocol 不能为空")
	}
	return nil
}

func ValidateConfiguration(config map[string]any) error {
	protocol, ok := config["protocol"].(string)
	if !ok || protocol == "" {
		return fmt.Errorf("protocol 不能为空")
	}
	port, ok := config["port"].(float64)
	if !ok || port != float64(int(port)) || port < 1 || port > 65535 {
		return fmt.Errorf("port 必须是 1–65535 的整数")
	}
	listen, ok := config["listen"].(string)
	if !ok {
		return fmt.Errorf("listen 必须是字符串")
	}
	if listen != "" && net.ParseIP(listen) == nil {
		return fmt.Errorf("listen 必须是 IP 地址或留空")
	}
	if _, ok := config["enable"].(bool); !ok {
		return fmt.Errorf("enable 必须是布尔值")
	}
	for _, key := range []string{"remark", "tag", "shareAddr", "shareAddrStrategy"} {
		if v, exists := config[key]; exists {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s 必须是字符串", key)
			}
		}
	}
	return nil
}

// WriteInbound never retries a mutation. Existing client accounts and traffic
// are copied from a fresh target read, never from the source configuration.
func (c *Client) WriteInbound(ctx context.Context, config map[string]any, existing *Inbound) (*Inbound, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	body, err := EditableConfiguration(string(raw))
	if err != nil {
		return nil, err
	}
	if err := ValidateConfiguration(body); err != nil {
		return nil, err
	}
	settings := body["settings"].(map[string]any)
	settings["clients"] = []any{}
	path := "/panel/api/inbounds/add"
	if existing != nil {
		var live map[string]any
		if err := json.Unmarshal([]byte(existing.Settings), &live); err != nil {
			return nil, fmt.Errorf("读取目标客户端: %w", err)
		}
		if clients, ok := live["clients"]; ok {
			settings["clients"] = clients
		}
		// Never reset inbound traffic or its reset bookkeeping during an edit.
		body["up"], body["down"] = existing.Up, existing.Down
		var target map[string]json.RawMessage
		_ = json.Unmarshal(existing.Raw, &target)
		for _, key := range []string{"lastTrafficResetTime", "nodeId", "originNodeGuid"} {
			if v, ok := target[key]; ok {
				body[key] = v
			}
		}
		body["id"] = existing.ID
		path = "/panel/api/inbounds/update/" + strconv.Itoa(existing.ID)
	}
	var out Inbound
	if err := c.do(ctx, "POST", path, body, &out); err != nil {
		return nil, err
	}
	if out.ID == 0 && existing != nil {
		out.ID = existing.ID
	}
	if out.ID == 0 {
		return nil, fmt.Errorf("面板未返回新入站 ID；请重新发现后核对，勿重复创建")
	}
	return &out, nil
}

// Modern panels expose enable changes separately from configuration edits.
func (c *Client) SetInboundEnabled(ctx context.Context, id int, enabled bool) error {
	return c.do(ctx, "POST", "/panel/api/inbounds/setEnable/"+strconv.Itoa(id), map[string]any{"enable": enabled}, nil)
}
