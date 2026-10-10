package panel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTemplateKeepsSharedSetupAndNodeSuppliesIngress(t *testing.T) {
	source, _ := EditableConfiguration(`{"protocol":"vless","listen":"127.0.0.1","port":9443,"enable":true,"tag":"in-9443","remark":"proxy node","shareAddr":"proxy.example.com","settings":{"decryption":"shared-suite","fallbacks":[{"dest":9444}]},"streamSettings":{"network":"tcp","security":"reality","tcpSettings":{"acceptProxyProtocol":true,"header":{"type":"none"}},"externalProxy":[{"port":443}],"realitySettings":{"privateKey":"shared-key","serverNames":["shared.example.com"],"limitFallbackUpload":{"afterBytes":65536,"bytesPerSec":8192}}}}`)
	before, _ := json.Marshal(source)
	template := TemplateConfiguration(source)
	if after, _ := json.Marshal(source); string(after) != string(before) {
		t.Fatal("source mutated")
	}
	for _, path := range nodePaths {
		if _, exists := configurationValue(template, strings.Split(path, ".")); exists {
			t.Fatalf("template kept node field %s", path)
		}
	}
	for _, path := range []string{"settings.decryption", "streamSettings.realitySettings.privateKey", "streamSettings.realitySettings.limitFallbackUpload", "streamSettings.tcpSettings.header"} {
		if _, exists := configurationValue(template, strings.Split(path, ".")); !exists {
			t.Fatalf("template lost shared field %s", path)
		}
	}
	direct := WithNode(template, NodeEnvironment{Port: 443, Remark: "direct"})
	if err := ValidateConfiguration(direct); err != nil {
		t.Fatal(err)
	}
	if v, _ := configurationValue(direct, []string{"streamSettings", "tcpSettings", "acceptProxyProtocol"}); v != nil {
		t.Fatal("direct node accepts PROXY headers")
	}
	proxied := WithNode(template, NodeEnvironment{Listen: "127.0.0.1", Port: 9443, AcceptProxyProtocol: true})
	if v, _ := configurationValue(proxied, []string{"streamSettings", "tcpSettings", "acceptProxyProtocol"}); v != true || proxied["listen"] != "127.0.0.1" {
		t.Fatal("proxied node lost its ingress")
	}
	if _, exists := direct["tag"]; exists {
		t.Fatal("tag should be derived by the panel")
	}
}

func TestFallbackWarnings(t *testing.T) {
	config, _ := EditableConfiguration(`{"streamSettings":{"security":"reality","realitySettings":{"limitFallbackUpload":{"bytesPerSec":8192},"limitFallbackDownload":{"bytesPerSec":0}}}}`)
	if len(FallbackWarnings(config)) != 1 {
		t.Fatal("unlimited download not reported")
	}
	config["streamSettings"].(map[string]any)["security"] = "tls"
	if len(FallbackWarnings(config)) != 0 {
		t.Fatal("TLS given REALITY warning")
	}
}

func TestMismatchedPathIgnoresPanelAdditionsAndDerivedTag(t *testing.T) {
	want, _ := EditableConfiguration(`{"port":443,"tag":"old","streamSettings":{"realitySettings":{"shortIds":["a"]}}}`)
	actual, _ := EditableConfiguration(`{"port":443,"tag":"inbound-443","allTime":5,"streamSettings":{"realitySettings":{"shortIds":["a"],"show":false}}}`)
	if path := MismatchedPath(want, actual); path != "" {
		t.Fatalf("unexpected mismatch %s", path)
	}
	actual["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["shortIds"] = []any{"b"}
	if path := MismatchedPath(want, actual); path != "streamSettings.realitySettings.shortIds" {
		t.Fatalf("mismatch = %q", path)
	}
	if !SameConfiguration(`{"id":1,"up":5,"port":443,"settings":{}}`, `{"port":443,"id":2,"down":9,"settings":{"clients":[]}}`) {
		t.Fatal("identity, counters or clients counted as drift")
	}
}
