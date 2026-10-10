package panel

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nodePaths describe one machine's ingress path rather than the shared
// protocol setup. Templates omit them; each new inbound supplies its own.
var nodePaths = []string{
	"listen", "port", "tag", "remark", "enable", "shareAddr", "shareAddrStrategy",
	"settings.fallbacks",
	"streamSettings.tcpSettings.acceptProxyProtocol",
	"streamSettings.rawSettings.acceptProxyProtocol",
	"streamSettings.sockopt.acceptProxyProtocol",
	"streamSettings.externalProxy",
}

// NodeEnvironment is what differs between nodes sharing a template. A node
// behind a reverse proxy listens on loopback and accepts PROXY headers; a
// node reached directly listens publicly without them.
type NodeEnvironment struct {
	Listen              string `json:"listen"`
	Port                int    `json:"port"`
	Remark              string `json:"remark"`
	AcceptProxyProtocol bool   `json:"acceptProxyProtocol"`
}

// TemplateConfiguration copies config without node-specific fields. Keys,
// encryption, REALITY target/SNI and fallback limits stay in the template.
func TemplateConfiguration(config map[string]any) map[string]any {
	result := deepCopy(config)
	for _, path := range nodePaths {
		setConfigurationValue(result, strings.Split(path, "."), nil, false)
	}
	return result
}

// WithNode builds a complete inbound configuration from a template. The panel
// derives the tag from listen and port, so none is sent.
func WithNode(template map[string]any, node NodeEnvironment) map[string]any {
	result := TemplateConfiguration(template)
	result["listen"] = node.Listen
	result["port"] = float64(node.Port)
	result["remark"] = node.Remark
	result["enable"] = true
	if node.AcceptProxyProtocol {
		stream, _ := result["streamSettings"].(map[string]any)
		if stream == nil {
			stream = map[string]any{}
			result["streamSettings"] = stream
		}
		network, _ := stream["network"].(string)
		if network == "" {
			network = "tcp"
		}
		setConfigurationValue(stream, []string{network + "Settings", "acceptProxyProtocol"}, true, true)
	}
	return result
}

func deepCopy(config map[string]any) map[string]any {
	raw, _ := json.Marshal(config)
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}

func configurationValue(config map[string]any, path []string) (any, bool) {
	for _, key := range path[:len(path)-1] {
		child, ok := config[key].(map[string]any)
		if !ok {
			return nil, false
		}
		config = child
	}
	value, exists := config[path[len(path)-1]]
	return value, exists
}

func setConfigurationValue(config map[string]any, path []string, value any, exists bool) {
	for _, key := range path[:len(path)-1] {
		child, ok := config[key].(map[string]any)
		if !ok {
			if !exists {
				return
			}
			child = map[string]any{}
			config[key] = child
		}
		config = child
	}
	key := path[len(path)-1]
	if !exists {
		delete(config, key)
		return
	}
	config[key] = value
}

// FallbackWarnings describe the effective configuration rather than claiming
// that historical limits are the current state or imposing them on every node.
func FallbackWarnings(config map[string]any) []string {
	security, _ := configurationValue(config, []string{"streamSettings", "security"})
	if security != "reality" {
		return nil
	}
	warnings := []string{}
	for _, direction := range []struct{ key, label string }{{"limitFallbackUpload", "上传"}, {"limitFallbackDownload", "下载"}} {
		value, _ := configurationValue(config, []string{"streamSettings", "realitySettings", direction.key, "bytesPerSec"})
		rate, ok := value.(float64)
		if !ok || rate <= 0 {
			warnings = append(warnings, fmt.Sprintf("REALITY 回落%s未设置有限速率，未认证的回落连接不受限速。", direction.label))
		}
	}
	return warnings
}
