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

func group(t *testing.T, cfg map[string]any, name string) map[string]any {
	t.Helper()
	for _, item := range cfg["proxy-groups"].([]any) {
		g, _ := item.(map[string]any)
		if g["name"] == name {
			return g
		}
	}
	t.Fatalf("group %q not found in %v", name, groupNames(t, cfg))
	return nil
}

func groupMembers(t *testing.T, cfg map[string]any, name string) []string {
	t.Helper()
	var out []string
	for _, m := range group(t, cfg, name)["proxies"].([]any) {
		out = append(out, m.(string))
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func sampleInput(t *testing.T) Input {
	t.Helper()
	return Input{
		Proxies: []Proxy{
			{Name: "HK-01", Entry: mustEntry(t, "vless://u@hk1.example:443?security=tls#hk1")},
			{Name: "HK-02", Entry: mustEntry(t, "vless://u@hk2.example:443?security=tls#hk2")},
		},
		Groups: []Group{
			{Name: "🤖 AI 服务", Type: "select", Members: []string{"🇭🇰 香港", "DIRECT"}},
			{Name: "🇭🇰 香港", Type: "url-test", Members: []string{"HK-01", "HK-02"}},
		},
		Rules: []string{"GEOSITE,openai,🤖 AI 服务", "MATCH,🤖 AI 服务"},
	}
}

func TestRenderEmitsResolvedGroupsAndRules(t *testing.T) {
	out, err := Render(DefaultTemplate, sampleInput(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)

	if got := groupNames(t, cfg); len(got) != 2 || got[0] != "🤖 AI 服务" {
		t.Errorf("proxy-groups = %v, want policies before node groups", got)
	}
	if got := groupMembers(t, cfg, "🇭🇰 香港"); len(got) != 2 || got[0] != "HK-01" {
		t.Errorf("node group members = %v", got)
	}
	if len(cfg["proxies"].([]any)) != 2 {
		t.Errorf("proxies = %d, want 2", len(cfg["proxies"].([]any)))
	}
	rules := cfg["rules"].([]any)
	if len(rules) != 2 || rules[len(rules)-1] != "MATCH,🤖 AI 服务" {
		t.Errorf("rules = %v, want the input lines verbatim", rules)
	}
}

func TestRenderProbeFieldsFollowGroupType(t *testing.T) {
	in := Input{
		Proxies: []Proxy{{Name: "A", Entry: mustEntry(t, "vless://u@a.example:443?security=tls#A")}},
		Groups: []Group{
			{Name: "manual", Type: "select", Members: []string{"A"}, TestURL: "https://ignored.example", Interval: 60},
			{Name: "auto", Type: "url-test", Members: []string{"A"}},
			{Name: "failover", Type: "fallback", Members: []string{"A"}},
		},
		Rules: []string{"MATCH,manual"},
	}
	cfg := decode(t, mustRender(t, in))

	// mihomo rejects `url` on a select group, so it must not leak through even
	// when the operator left probe fields filled in.
	if _, ok := group(t, cfg, "manual")["url"]; ok {
		t.Error("select group carries a url")
	}
	auto := group(t, cfg, "auto")
	if auto["url"] != DefaultTestURL || auto["interval"] != DefaultInterval || auto["tolerance"] != DefaultTolerance {
		t.Errorf("url-test group = %v, want probe defaults filled in", auto)
	}
	if _, ok := group(t, cfg, "failover")["tolerance"]; ok {
		t.Error("fallback group carries a tolerance, which only url-test reads")
	}
}

func TestRenderPrunesEmptyGroupsAndRepointsRules(t *testing.T) {
	// An expired user still fetches their subscription. mihomo rejects an empty
	// proxy-group, so the node group goes, which empties the policy that only
	// referenced it, and the rules have to land somewhere that still exists.
	in := Input{
		Proxies: []Proxy{{Name: "A", Entry: mustEntry(t, "vless://u@a.example:443?security=tls#A")}},
		Groups: []Group{
			{Name: "keep", Type: "select", Members: []string{"A"}},
			{Name: "media", Type: "select", Members: []string{"empty"}},
			{Name: "empty", Type: "url-test"},
		},
		Rules: []string{"GEOSITE,netflix,media", "MATCH,media"},
	}
	cfg := decode(t, mustRender(t, in))

	if names := groupNames(t, cfg); len(names) != 1 || names[0] != "keep" {
		t.Fatalf("proxy-groups = %v, want only the group that had members", names)
	}
	for _, rule := range cfg["rules"].([]any) {
		parts := strings.Split(rule.(string), ",")
		if target := strings.TrimSpace(parts[targetIndex(parts)]); target != "keep" {
			t.Errorf("rule %q points at %q, want the surviving group", rule, target)
		}
	}
}

func TestRenderWithNothingToServeStaysLoadable(t *testing.T) {
	out, err := Render(DefaultTemplate, Input{Rules: []string{"MATCH,gone"}})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)
	if proxies, ok := cfg["proxies"].([]any); ok && len(proxies) != 0 {
		t.Errorf("proxies = %v, want empty", proxies)
	}
	if groups, ok := cfg["proxy-groups"].([]any); ok && len(groups) != 0 {
		t.Errorf("proxy-groups = %v, want empty", groups)
	}
	if rules := cfg["rules"].([]any); len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Errorf("rules = %v, want MATCH repointed to DIRECT", rules)
	}
}

func TestRenderOverwritesWhateverTheTemplateCarries(t *testing.T) {
	// Templates from the token era still hold groups, rules and a firex block.
	// None of it may reach the client.
	template := `mixed-port: 7890
firex:
  region-group-type: url-test
proxy-groups:
  - name: stale
    type: select
    proxies: ['<ALL>']
rules:
  - MATCH,stale
`
	cfg := decode(t, mustRenderWith(t, template, sampleInput(t)))
	if _, ok := cfg["firex"]; ok {
		t.Error("firex block leaked into the rendered config")
	}
	if contains(groupNames(t, cfg), "stale") {
		t.Errorf("template proxy-groups survived: %v", groupNames(t, cfg))
	}
	if cfg["mixed-port"] != 7890 {
		t.Errorf("mixed-port = %v, want the template's base config kept", cfg["mixed-port"])
	}
}

func TestRenderRejectsBrokenTemplate(t *testing.T) {
	if _, err := Render("proxies: [\n", Input{}); err == nil {
		t.Fatal("Render() error = nil, want a YAML parse error")
	}
	if _, err := Render("- not a mapping", Input{}); err == nil {
		t.Fatal("Render() error = nil, want a root-must-be-a-mapping error")
	}
}

func mustRender(t *testing.T, in Input) string {
	t.Helper()
	return mustRenderWith(t, DefaultTemplate, in)
}

func mustRenderWith(t *testing.T, template string, in Input) string {
	t.Helper()
	out, err := Render(template, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return out
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
