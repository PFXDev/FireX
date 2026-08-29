package clash

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/sharelink"
)

func mustParse(t *testing.T, raw string) *sharelink.Proxy {
	t.Helper()
	p, err := sharelink.Parse(raw)
	if err != nil {
		t.Fatalf("sharelink.Parse(%q) error = %v", raw, err)
	}
	return p
}

func mustEntry(t *testing.T, raw string) *Ordered {
	t.Helper()
	entry, ok := ProxyEntry(mustParse(t, raw))
	if !ok {
		t.Fatalf("ProxyEntry(%q) not renderable", raw)
	}
	return entry
}

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("rendered config is not valid YAML: %v\n%s", err, out)
	}
	return got
}

func groupNames(t *testing.T, cfg map[string]any) []string {
	t.Helper()
	raw, ok := cfg["proxy-groups"].([]any)
	if !ok {
		t.Fatalf("proxy-groups missing or not a list: %#v", cfg["proxy-groups"])
	}
	var names []string
	for _, item := range raw {
		g, _ := item.(map[string]any)
		names = append(names, g["name"].(string))
	}
	return names
}

func groupMembers(t *testing.T, cfg map[string]any, name string) []string {
	t.Helper()
	for _, item := range cfg["proxy-groups"].([]any) {
		g, _ := item.(map[string]any)
		if g["name"] != name {
			continue
		}
		var out []string
		for _, m := range g["proxies"].([]any) {
			out = append(out, m.(string))
		}
		return out
	}
	t.Fatalf("group %q not found in %v", name, groupNames(t, cfg))
	return nil
}

func TestRenderExpandsRegionGroups(t *testing.T) {
	in := Input{Nodes: []Node{
		{Name: "🇭🇰 HK-01", Region: "🇭🇰 香港", Entry: mustEntry(t, "vless://u1@hk1.example:443?security=tls#hk1")},
		{Name: "🇭🇰 HK-02", Region: "🇭🇰 香港", Entry: mustEntry(t, "vless://u1@hk2.example:443?security=tls#hk2")},
		{Name: "🇯🇵 JP-01", Region: "🇯🇵 日本", Entry: mustEntry(t, "vless://u1@jp1.example:443?security=tls#jp1")},
	}}
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)

	names := groupNames(t, cfg)
	if !contains(names, "🇭🇰 香港") || !contains(names, "🇯🇵 日本") {
		t.Fatalf("region groups missing, got %v", names)
	}
	if got := groupMembers(t, cfg, "🇭🇰 香港"); len(got) != 2 || got[0] != "🇭🇰 HK-01" {
		t.Errorf("HK members = %v, want the two HK nodes", got)
	}
	if got := groupMembers(t, cfg, "♻️ 自动选择"); len(got) != 3 {
		t.Errorf("auto-select members = %v, want all 3 nodes", got)
	}
	// The selector must offer the region groups, not just the raw nodes.
	sel := groupMembers(t, cfg, "🚀 节点选择")
	if !contains(sel, "🇭🇰 香港") || !contains(sel, "🇯🇵 日本") {
		t.Errorf("selector members = %v, want region groups included", sel)
	}
	if len(cfg["proxies"].([]any)) != 3 {
		t.Errorf("proxies = %d, want 3", len(cfg["proxies"].([]any)))
	}
}

func TestRenderDedupesSelectorEntries(t *testing.T) {
	// <REGION_GROUPS> then <ALL> would list an ungrouped node twice otherwise.
	in := Input{Nodes: []Node{
		{Name: "A", Region: "", Entry: mustEntry(t, "vless://u@a.example:443?security=tls#A")},
	}}
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	members := groupMembers(t, decode(t, out), "🚀 节点选择")
	seen := map[string]int{}
	for _, m := range members {
		seen[m]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("member %q appears %d times in %v", name, n, members)
		}
	}
}

func TestRenderWithNoNodesStaysValid(t *testing.T) {
	// An expired user still fetches their subscription; mihomo rejects an empty
	// proxy-group, so those groups must be dropped and the rules repointed.
	out, err := Render(DefaultTemplate, Input{})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)

	if proxies, ok := cfg["proxies"].([]any); ok && len(proxies) != 0 {
		t.Errorf("proxies = %v, want empty", proxies)
	}
	for _, name := range groupNames(t, cfg) {
		if len(groupMembers(t, cfg, name)) == 0 {
			t.Errorf("group %q rendered empty", name)
		}
	}
	if contains(groupNames(t, cfg), "♻️ 自动选择") {
		t.Error("auto-select group survived with no nodes to test")
	}
	for _, rule := range cfg["rules"].([]any) {
		parts := strings.Split(rule.(string), ",")
		target := strings.TrimSpace(parts[targetIndex(parts)])
		if target == "♻️ 自动选择" {
			t.Errorf("rule %q still points at a dropped group", rule)
		}
	}
}

func TestRenderFilterAndTagTokens(t *testing.T) {
	template := `proxy-groups:
  - name: streaming
    type: select
    proxies:
      - <TAG:media>
  - name: hk-only
    type: select
    proxies:
      - <FILTER:^HK>
rules:
  - MATCH,streaming
`
	in := Input{Nodes: []Node{
		{Name: "HK-01", Tags: []string{"media", "premium"}, Entry: mustEntry(t, "vless://u@a.example:443?security=tls#a")},
		{Name: "JP-01", Tags: []string{"basic"}, Entry: mustEntry(t, "vless://u@b.example:443?security=tls#b")},
	}}
	out, err := Render(template, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)
	if got := groupMembers(t, cfg, "streaming"); len(got) != 1 || got[0] != "HK-01" {
		t.Errorf("streaming = %v, want [HK-01]", got)
	}
	if got := groupMembers(t, cfg, "hk-only"); len(got) != 1 || got[0] != "HK-01" {
		t.Errorf("hk-only = %v, want [HK-01]", got)
	}
}

func TestRenderRejectsBadFilter(t *testing.T) {
	template := "proxy-groups:\n  - name: g\n    type: select\n    proxies: ['<FILTER:[unclosed>']\n"
	if _, err := Render(template, Input{}); err == nil {
		t.Fatal("Render() error = nil, want a regexp compile error")
	}
}

func TestProxyEntryRealityFields(t *testing.T) {
	entry := mustEntry(t, "vless://uuid-x@n.example:443?type=tcp&security=reality&pbk=PBK&sid=aa&fp=chrome&sni=www.apple.com&flow=xtls-rprx-vision#N")
	if v, _ := entry.Get("flow"); v != "xtls-rprx-vision" {
		t.Errorf("flow = %v", v)
	}
	if v, _ := entry.Get("servername"); v != "www.apple.com" {
		t.Errorf("servername = %v", v)
	}
	if _, ok := entry.Get("skip-cert-verify"); ok {
		t.Error("skip-cert-verify set on a reality proxy")
	}
	reality, ok := entry.Get("reality-opts")
	if !ok {
		t.Fatal("reality-opts missing")
	}
	pk, _ := reality.(*Ordered).Get("public-key")
	if pk != "PBK" {
		t.Errorf("public-key = %v, want PBK", pk)
	}
}

func TestProxyEntryWebSocketHeaders(t *testing.T) {
	entry := mustEntry(t, "vless://u@cdn.example:443?type=ws&security=tls&path=%2Fp&host=front.example#N")
	opts, ok := entry.Get("ws-opts")
	if !ok {
		t.Fatal("ws-opts missing")
	}
	path, _ := opts.(*Ordered).Get("path")
	if path != "/p" {
		t.Errorf("path = %v, want /p", path)
	}
	headers, _ := opts.(*Ordered).Get("headers")
	host, _ := headers.(*Ordered).Get("Host")
	if host != "front.example" {
		t.Errorf("Host = %v", host)
	}
}

func TestProxyEntryUnsupportedTransport(t *testing.T) {
	// mihomo has no xhttp transport; emitting the proxy anyway would produce a
	// config that fails to load rather than one node short.
	if _, ok := ProxyEntry(mustParse(t, "vless://u@h.example:443?type=xhttp&security=tls#N")); ok {
		t.Error("ProxyEntry() accepted an xhttp proxy")
	}
}

func TestProxyEntryShadowsocksWithPluginIsSkipped(t *testing.T) {
	raw := "ss://YWVzLTI1Ni1nY206cHc@h.example:8388?plugin=obfs-local%3Bobfs%3Dhttp#N"
	if _, ok := ProxyEntry(mustParse(t, raw)); ok {
		t.Error("ProxyEntry() accepted an ss proxy whose plugin options were dropped")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
