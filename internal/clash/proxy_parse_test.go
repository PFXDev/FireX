package clash

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseProxiesChecksRequiredConnectionFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string]any
		valid  bool
	}{
		{"vless", map[string]any{"type": "vless", "uuid": "test-uuid"}, true},
		{"vmess", map[string]any{"type": "vmess", "uuid": "test-uuid"}, true},
		{"trojan", map[string]any{"type": "trojan", "password": "secret"}, true},
		{"hysteria2", map[string]any{"type": "hysteria2", "password": "secret", "port": "443"}, true},
		{"hysteria2 port hopping", map[string]any{"type": "hysteria2", "ports": "443,8443-8445", "port": nil}, true},
		{"ss", map[string]any{"type": "ss", "cipher": "aes-128-gcm", "password": "secret"}, true},
		{"ss without encryption", map[string]any{"type": "ss", "cipher": "none"}, true},
		{"tuic", map[string]any{"type": "tuic", "uuid": "test-uuid", "password": "secret"}, true},
		{"tuic token", map[string]any{"type": "tuic", "token": "secret"}, true},
		{"missing uuid", map[string]any{"type": "vless"}, false},
		{"missing password", map[string]any{"type": "trojan"}, false},
		{"missing cipher", map[string]any{"type": "ss", "password": "secret"}, false},
		{"blank server", map[string]any{"type": "vless", "uuid": "test-uuid", "server": " "}, false},
		{"missing port", map[string]any{"type": "vless", "uuid": "test-uuid", "port": nil}, false},
		{"invalid port", map[string]any{"type": "vless", "uuid": "test-uuid", "port": 65536}, false},
		{"direct", map[string]any{"type": "direct", "server": nil, "port": nil}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := map[string]any{"name": "node", "server": "example.com", "port": 443}
			for key, value := range tc.fields {
				proxy[key] = value
			}
			payload, err := yaml.Marshal(map[string]any{"proxies": []any{proxy}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseProxies(payload)
			if (err == nil) != tc.valid {
				t.Fatalf("ParseProxies error = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestParseProxiesYAMLAliases(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		valid         bool
	}{
		{"cycle", "cycle: &cycle [*cycle]\nproxies: []\n", false},
		{"shared scalar", "host: &host example.com\nproxies:\n  - {name: node, type: trojan, server: *host, port: 443, password: secret}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxies, err := ParseProxies([]byte(tc.payload))
			if (err == nil) != tc.valid {
				t.Fatalf("ParseProxies error = %v, want valid=%v", err, tc.valid)
			}
			if tc.valid {
				server, _ := proxies[0].Entry.Get("server")
				if server != "example.com" {
					t.Fatalf("alias resolved to %v", server)
				}
			}
		})
	}
}
