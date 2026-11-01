package clash

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseProxies reads the proxies sequence from a mihomo configuration. The
// proxy mappings retain every upstream option and their original key order.
func ParseProxies(payload []byte) ([]Proxy, error) {
	root, err := parseProxyMapping(payload)
	if err != nil {
		return nil, err
	}
	raw, ok := root.Get("proxies")
	if !ok {
		return nil, fmt.Errorf("clash: missing proxies")
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("clash: proxies must be a sequence")
	}
	out := make([]Proxy, 0, len(items))
	for i, item := range items {
		entry, ok := item.(*Ordered)
		if !ok {
			return nil, fmt.Errorf("clash: proxy %d must be a mapping", i+1)
		}
		name, err := validateProxy(entry)
		if err != nil {
			return nil, fmt.Errorf("clash: proxy %d: %w", i+1, err)
		}
		out = append(out, Proxy{Name: name, Entry: entry})
	}
	return out, nil
}

// ParseProxy reads one cached mihomo proxy mapping.
func ParseProxy(payload []byte) (*Ordered, error) {
	entry, err := parseProxyMapping(payload)
	if err != nil {
		return nil, err
	}
	if _, err := validateProxy(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func parseProxyMapping(payload []byte) (*Ordered, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("clash: parse proxies: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("clash: empty proxy configuration")
	}
	// Decoding into a Node alone skips yaml.v3's alias cycle/expansion checks.
	// Run its normal decoder before traversing aliases to preserve key order.
	var decoded any
	if err := doc.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("clash: parse proxies: %w", err)
	}
	value, err := nodeToValue(doc.Content[0])
	if err != nil {
		return nil, fmt.Errorf("clash: parse proxies: %w", err)
	}
	root, ok := value.(*Ordered)
	if !ok {
		return nil, fmt.Errorf("clash: proxy configuration must be a mapping")
	}
	return root, nil
}

// Check the connection fields of the protocols FireX also accepts as share
// links. Leave protocol-specific options and other mihomo types intact.
func validateProxy(entry *Ordered) (string, error) {
	name, err := requiredProxyField(entry, "name")
	if err != nil {
		return "", err
	}
	protocol, err := requiredProxyField(entry, "type")
	if err != nil {
		return "", err
	}
	var credentials []string
	switch protocol {
	case "vless", "vmess":
		credentials = []string{"uuid"}
	case "trojan":
		credentials = []string{"password"}
	case "hysteria2":
		// Password may be omitted; the server chooses its authentication policy.
	case "ss":
		credentials = []string{"cipher"}
	case "tuic":
		if _, err := requiredProxyField(entry, "token"); err != nil {
			credentials = []string{"uuid"}
		}
	default:
		return name, nil
	}
	if _, err := requiredProxyField(entry, "server"); err != nil {
		return "", err
	}
	rawPort, _ := entry.Get("port")
	port, err := strconv.Atoi(fmt.Sprint(rawPort))
	_, portsErr := requiredProxyField(entry, "ports")
	portHopping := protocol == "hysteria2" && portsErr == nil
	if !portHopping && (err != nil || port < 1 || port > 65535) {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	for _, key := range credentials {
		if _, err := requiredProxyField(entry, key); err != nil {
			return "", err
		}
	}
	return name, nil
}

func requiredProxyField(entry *Ordered, key string) (string, error) {
	raw, ok := entry.Get(key)
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return value, nil
}
