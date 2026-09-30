package panel

import (
	"encoding/json"
	"strings"
)

const VisionFlow = "xtls-rprx-vision"

func (i *Inbound) Transport() (network, security string) {
	var stream struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	}
	if json.Unmarshal([]byte(i.StreamSettings), &stream) != nil {
		return "", ""
	}
	return strings.ToLower(stream.Network), strings.ToLower(stream.Security)
}

// VisionReason is shared by validation and the UI. Limit the switch to the
// VLESS TCP/RAW + TLS/REALITY combination supported by our subscription targets.
func VisionReason(protocol, network, security string, disabled bool) string {
	if !strings.EqualFold(protocol, "vless") {
		return "Vision 仅适用于 VLESS 入站"
	}
	if network == "" || security == "" {
		return "请先同步面板参数，确认传输与安全设置"
	}
	if network != "tcp" && network != "raw" {
		return "Vision 需要 TCP / RAW 传输"
	}
	if security != "tls" && security != "reality" {
		return "Vision 需要 TLS 或 REALITY"
	}
	if disabled {
		return "此入站在 3X-UI 中禁用了流控，请先取消 Disable flow"
	}
	return ""
}

// Configuration preserves unknown panel options and normalizes JSON-backed
// fields for the inspector. User accounts and their counters belong to the
// users view, not to a snapshot of the inbound's connection parameters.
func (i *Inbound) Configuration() string {
	var config map[string]json.RawMessage
	_ = json.Unmarshal(i.Raw, &config)
	if config == nil {
		data, _ := json.Marshal(i)
		_ = json.Unmarshal(data, &config)
	}
	delete(config, "clientStats")
	for key, raw := range map[string]string{
		"settings": i.Settings, "streamSettings": i.StreamSettings, "sniffing": i.Sniffing,
	} {
		var value any
		if json.Unmarshal([]byte(raw), &value) != nil {
			value = raw
		}
		if key == "settings" {
			if settings, ok := value.(map[string]any); ok {
				delete(settings, "clients")
			}
		}
		config[key], _ = json.Marshal(value)
	}
	data, _ := json.Marshal(config)
	return string(data)
}

// ClientFlows reads the actual per-inbound values, rather than the panel's
// canonical client record which can only represent one flow.
func (i *Inbound) ClientFlows() map[string]string {
	var settings struct {
		Clients []struct {
			Email string `json:"email"`
			Flow  string `json:"flow"`
		} `json:"clients"`
	}
	_ = json.Unmarshal([]byte(i.Settings), &settings)
	out := make(map[string]string, len(settings.Clients))
	for _, client := range settings.Clients {
		out[strings.ToLower(client.Email)] = client.Flow
	}
	return out
}
