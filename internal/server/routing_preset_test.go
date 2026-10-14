package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
)

type presetConfig struct {
	IPv6    bool `yaml:"ipv6"`
	Proxies []struct {
		Name      string `yaml:"name"`
		Type      string `yaml:"type"`
		IPVersion string `yaml:"ip-version"`
	} `yaml:"proxies"`
	Groups []struct {
		Name    string   `yaml:"name"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
	Rules         []string `yaml:"rules"`
	RuleProviders map[string]struct {
		Type     string `yaml:"type"`
		Behavior string `yaml:"behavior"`
		Format   string `yaml:"format"`
		URL      string `yaml:"url"`
		Interval int    `yaml:"interval"`
	} `yaml:"rule-providers"`
	DNS struct {
		EnhancedMode string `yaml:"enhanced-mode"`
		RespectRules bool   `yaml:"respect-rules"`
	} `yaml:"dns"`
}

func (h *harness) subscription(token string) presetConfig {
	h.t.Helper()
	var cfg presetConfig
	if err := yaml.Unmarshal(h.mustDo(http.MethodGet, "/sub/"+token+"?target=mihomo", nil), &cfg); err != nil {
		h.t.Fatal(err)
	}
	return cfg
}

// Every built-in layout has to pass the same validation an edited matrix
// does, and the stock one has to render the separate AI rows, the IPv6-first
// direct outbound it references, and a mainland check that resolves.
func TestRoutingPresetsApplyAndRender(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	cfg := h.subscription(u.SubToken)
	if !cfg.IPv6 || cfg.DNS.EnhancedMode != "fake-ip" || !cfg.DNS.RespectRules {
		t.Fatalf("stock template: ipv6=%v enhanced-mode=%q respect-rules=%v", cfg.IPv6, cfg.DNS.EnhancedMode, cfg.DNS.RespectRules)
	}
	direct := cfg.Proxies[len(cfg.Proxies)-1]
	if direct.Name != model.BuiltinDirectV6 || direct.Type != "direct" || direct.IPVersion != "ipv6-prefer" {
		t.Fatalf("last proxy = %+v, want the ipv6-prefer direct outbound", direct)
	}
	for _, want := range []string{
		"GEOSITE,openai,🤖 OpenAI",
		"GEOSITE,anthropic,✳️ Claude",
		"GEOIP,cn,🎯 全球直连",
		"MATCH,🐟 漏网之鱼",
	} {
		if !slices.Contains(cfg.Rules, want) {
			t.Errorf("rules lack %q: %v", want, cfg.Rules)
		}
	}
	assertStandardServiceRouting(t, cfg)

	for _, preset := range routing.Presets {
		h.mustDo(http.MethodPost, "/api/routing/presets/"+preset.Key, nil)
		cfg := h.subscription(u.SubToken)
		if preset.Key == "standard" {
			assertStandardServiceRouting(t, cfg)
		}
		if len(cfg.Rules) == 0 || cfg.Rules[len(cfg.Rules)-1] != "MATCH,🐟 漏网之鱼" {
			t.Errorf("preset %s: rules = %v", preset.Key, cfg.Rules)
		}
	}
	if resp, _ := h.do(http.MethodPost, "/api/routing/presets/nope", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown preset: status %d, want 404", resp.StatusCode)
	}
}

// The overlapping service lists only work in one order: whichever row matches
// first takes the connection, so each narrower row must precede the broader
// list that also names its hosts.
func assertStandardServiceRouting(t *testing.T, cfg presetConfig) {
	t.Helper()
	firstRule := func(target string) int {
		return slices.IndexFunc(cfg.Rules, func(rule string) bool {
			return strings.HasSuffix(rule, ","+target) || strings.Contains(rule, ","+target+",")
		})
	}
	for _, pair := range [][2]string{
		{"📥 大流量下载", "🤖 OpenAI"},
		{"📥 大流量下载", "✨ AI 服务"},
		{"📥 大流量下载", "🔎 谷歌服务"},
		{"✨ AI 服务", "🔎 谷歌服务"},
		{"📺 国外媒体", "🔎 谷歌服务"},
		{"🕹️ 游戏下载", "🎮 Steam"},
		{"🔎 谷歌服务", "🌐 国外网站"},
		{"🎮 Steam", "🌐 国外网站"},
	} {
		first, second := firstRule(pair[0]), firstRule(pair[1])
		if first < 0 || second < 0 || first >= second {
			t.Errorf("want %s rules before %s, indexes = %d, %d", pair[0], pair[1], first, second)
		}
	}
	if !slices.Contains(cfg.Rules, "RULE-SET,大流量下载,📥 大流量下载") {
		t.Errorf("大流量下载 does not use its rule set: %v", cfg.Rules)
	}
	if p, ok := cfg.RuleProviders["大流量下载"]; !ok || p.Type != "http" || p.Behavior != "classical" || p.Format != "text" || !strings.Contains(p.URL, "/rules/") {
		t.Errorf("rule-provider 大流量下载 = %+v, want an http classical text provider served by FireX", p)
	}
	for _, want := range []struct {
		name  string
		first string
	}{
		{"🔎 谷歌服务", "🚀 节点选择"},
		{"📥 大流量下载", "🚀 节点选择"},
		{"🕹️ 游戏下载", "DIRECT"},
		{"🎮 Steam", "🚀 节点选择"},
	} {
		index := slices.IndexFunc(cfg.Groups, func(g struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		}) bool {
			return g.Name == want.name
		})
		if index < 0 {
			t.Errorf("groups lack %q", want.name)
			continue
		}
		members := cfg.Groups[index].Proxies
		if len(members) == 0 || members[0] != want.first {
			t.Errorf("%s members = %v, want %s first", want.name, members, want.first)
		}
		if want.name == "🎮 Steam" && !slices.Contains(members, "DIRECT") {
			t.Error("Steam group lacks DIRECT")
		}
	}
}

// Swapping layouts must not undo a tier's choices where the new layout still
// has the row, and must not leave a column pointing at a policy that is gone.
func TestRoutingPresetKeepsProfileOverrides(t *testing.T) {
	h := newHarness(t)
	h.seed()
	var profile model.Profile
	if err := h.db.First(&profile, "name = ?", "标准分流").Error; err != nil {
		t.Fatal(err)
	}

	doc := h.routingDoc()
	policies := doc["policies"].([]any)
	indexOf := func(name string) int {
		for i, p := range policies {
			if p.(map[string]any)["name"] == name {
				return i
			}
		}
		t.Fatalf("stock matrix has no %s", name)
		return -1
	}
	doc["egresses"] = append(doc["egresses"].([]any),
		map[string]any{
			"policyIndex": indexOf("Claude"), "profileId": profile.ID, "type": "select",
			"members": []map[string]string{{"kind": "node-group", "ref": "🇯🇵 日本"}},
		},
		map[string]any{
			"policyIndex": indexOf("国外网站"), "profileId": profile.ID, "type": "select",
			"members": []map[string]string{{"kind": "policy", "ref": "国外媒体"}},
		},
	)
	h.mustDo(http.MethodPut, "/api/routing", doc)
	h.mustDo(http.MethodPost, "/api/routing/presets/lite", nil)

	overrides := map[string][]model.EgressMember{}
	var egresses []model.Egress
	h.db.Where("profile_id = ?", profile.ID).Find(&egresses)
	for _, e := range egresses {
		var policy model.Policy
		h.db.First(&policy, e.PolicyID)
		var members []model.EgressMember
		h.db.Where("egress_id = ?", e.ID).Find(&members)
		overrides[policy.Name] = members
	}
	if got := overrides["Claude"]; len(got) != 1 || got[0].Ref != "🇯🇵 日本" {
		t.Errorf("Claude override = %+v, want it kept", got)
	}
	if got, ok := overrides["国外网站"]; ok {
		t.Errorf("国外网站 override = %+v, want it dropped once its only member is gone", got)
	}
}
