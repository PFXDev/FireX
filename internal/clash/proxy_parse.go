package clash

import (
	"fmt"

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
		name, err := requiredProxyField(entry, "name")
		if err != nil {
			return nil, fmt.Errorf("clash: proxy %d: %w", i+1, err)
		}
		if _, err := requiredProxyField(entry, "type"); err != nil {
			return nil, fmt.Errorf("clash: proxy %q: %w", name, err)
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
	if _, err := requiredProxyField(entry, "name"); err != nil {
		return nil, err
	}
	if _, err := requiredProxyField(entry, "type"); err != nil {
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

func requiredProxyField(entry *Ordered, key string) (string, error) {
	raw, ok := entry.Get(key)
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	value, ok := raw.(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return value, nil
}
