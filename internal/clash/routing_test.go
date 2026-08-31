package clash

import (
	"strings"
	"testing"
)

func testGroup(name string, members ...string) Group {
	return Group{Name: name, Display: name, Type: "url-test", Members: members}
}

func routingInput(t *testing.T, groups []Group, names ...string) Input {
	t.Helper()
	nodes := make([]Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, Node{
			Name:  name,
			Entry: mustEntry(t, "vless://u@h.example:443?security=tls#"+name),
		})
	}
	return Input{Nodes: nodes, Groups: groups, Routing: DefaultRouting()}
}

func rules(t *testing.T, cfg map[string]any) []string {
	t.Helper()
	raw, ok := cfg["rules"].([]any)
	if !ok {
		t.Fatalf("rules missing or not a list: %#v", cfg["rules"])
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(string))
	}
	return out
}

func TestRoutingRendersPolicyGroupsThenNodeGroups(t *testing.T) {
	in := routingInput(t,
		[]Group{testGroup("🇭🇰 香港 IEPL", "HK-01", "HK-02"), testGroup("🇯🇵 日本", "JP-01")},
		"HK-01", "HK-02", "JP-01",
	)
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)

	names := groupNames(t, cfg)
	if names[0] != "🚀 节点选择" {
		t.Errorf("first group = %q, want the selector", names[0])
	}
	hk, jp := indexOf(names, "🇭🇰 香港 IEPL"), indexOf(names, "🇯🇵 日本")
	if hk < 0 || jp < 0 {
		t.Fatalf("node groups missing from %v", names)
	}
	if hk > jp {
		t.Errorf("node groups out of order: %v", names)
	}
	if last := indexOf(names, "🐟 漏网之鱼"); last > hk {
		t.Errorf("policy groups must precede node groups, got %v", names)
	}

	// <all-groups> inside the selector must list the node groups, not the nodes.
	sel := groupMembers(t, cfg, "🚀 节点选择")
	if !contains(sel, "🇭🇰 香港 IEPL") || !contains(sel, "🇯🇵 日本") {
		t.Errorf("selector = %v, want both node groups", sel)
	}
	if got := groupMembers(t, cfg, "♻️ 自动选择"); len(got) != 3 {
		t.Errorf("auto-select = %v, want all three nodes", got)
	}
	if got := groupMembers(t, cfg, "🇭🇰 香港 IEPL"); len(got) != 2 || got[0] != "HK-01" {
		t.Errorf("HK group = %v, want its two members in order", got)
	}
}

func TestRoutingRulesUseDisplayNames(t *testing.T) {
	in := routingInput(t, []Group{testGroup("🇭🇰 香港", "HK-01")}, "HK-01")
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	lines := rules(t, decode(t, out))

	if !contains(lines, "GEOSITE,openai,🤖 AI 服务") {
		t.Errorf("AI rule missing or not using the display name: %v", lines)
	}
	if !contains(lines, "GEOIP,telegram,📱 电报消息,no-resolve") {
		t.Errorf("no-resolve flag missing: %v", lines)
	}
	// no-resolve is meaningless on a domain matcher and must not leak onto one.
	for _, line := range lines {
		if strings.HasPrefix(line, "GEOSITE,") && strings.HasSuffix(line, ",no-resolve") {
			t.Errorf("no-resolve on a domain rule: %q", line)
		}
	}
	if last := lines[len(lines)-1]; last != "MATCH,🐟 漏网之鱼" {
		t.Errorf("last rule = %q, want the final policy", last)
	}
}

func TestRoutingPrunesGroupWithNoUsableMembers(t *testing.T) {
	// A group whose nodes are all outside this user's plan renders empty, and
	// mihomo refuses to load a config with an empty proxy-group.
	in := routingInput(t, []Group{testGroup("🇭🇰 香港", "HK-01"), testGroup("🇸🇬 新加坡")}, "HK-01")
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)
	if contains(groupNames(t, cfg), "🇸🇬 新加坡") {
		t.Error("empty node group survived rendering")
	}
	for _, name := range groupNames(t, cfg) {
		if len(groupMembers(t, cfg, name)) == 0 {
			t.Errorf("group %q rendered empty", name)
		}
	}
}

func TestRoutingWithNoNodesStaysLoadable(t *testing.T) {
	// An expired user still fetches their subscription. Groups that had only
	// nodes to offer disappear; the ones that also offer DIRECT survive, and
	// no rule may point at a name that is gone.
	out, err := Render(DefaultTemplate, Input{Routing: DefaultRouting()})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cfg := decode(t, out)
	names := groupNames(t, cfg)
	if contains(names, "♻️ 自动选择") {
		t.Error("auto-select survived with no nodes to test")
	}
	for _, name := range names {
		if len(groupMembers(t, cfg, name)) == 0 {
			t.Errorf("group %q rendered empty", name)
		}
	}
	for _, line := range rules(t, cfg) {
		parts := strings.Split(line, ",")
		target := strings.TrimSpace(parts[targetIndex(parts)])
		if contains(BuiltinPolicies, target) || contains(names, target) {
			continue
		}
		t.Errorf("rule %q points at %q, which is neither a group nor a built-in", line, target)
	}
}

func TestRoutingSelectGroupHasNoProbeFields(t *testing.T) {
	// mihomo rejects `url`/`interval` on a select group.
	in := routingInput(t, nil, "A")
	out, err := Render(DefaultTemplate, in)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, item := range decode(t, out)["proxy-groups"].([]any) {
		g := item.(map[string]any)
		if g["type"] != "select" {
			continue
		}
		if _, ok := g["url"]; ok {
			t.Errorf("select group %v carries a test url", g["name"])
		}
	}
}

func TestRoutingValidate(t *testing.T) {
	groups := []string{"🇭🇰 香港"}
	cases := []struct {
		name    string
		mutate  func(*Routing)
		wantErr string
	}{
		{"unknown policy", func(r *Routing) {
			r.Groups[0].Members = []Member{{Kind: MemberPolicy, Ref: "nope"}}
		}, "unknown policy group"},
		{"unknown node group", func(r *Routing) {
			r.Rules[0].Target = Member{Kind: MemberNodeGroup, Ref: "🇰🇷 韩国"}
		}, "unknown node group"},
		{"comma in name", func(r *Routing) { r.Groups[0].Name = "a,b" }, "comma"},
		{"bad type", func(r *Routing) { r.Groups[0].Type = "smart" }, "unknown type"},
		{"empty rule value", func(r *Routing) { r.Rules[0].Value = "  " }, "needs a value"},
		{"unknown matcher", func(r *Routing) { r.Rules[0].Type = "DOMAIN-GLOB" }, "unknown matcher"},
		{"expansion as target", func(r *Routing) {
			r.Rules[0].Target = Member{Kind: MemberAllNodes}
		}, "cannot be a rule target"},
		{"collides with node group", func(r *Routing) {
			r.Groups[0].Icon, r.Groups[0].Name = "🇭🇰", "香港"
		}, "collides with node group"},
		{"self reference", func(r *Routing) {
			r.Groups[1].Members = []Member{{Kind: MemberPolicy, Ref: r.Groups[1].Name}}
		}, "loop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := DefaultRouting()
			tc.mutate(r)
			err := r.Validate(groups)
			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}

	if err := DefaultRouting().Validate(groups); err != nil {
		t.Errorf("DefaultRouting() does not validate: %v", err)
	}
}

func TestRenameNodeGroupFollowsReferences(t *testing.T) {
	r := DefaultRouting()
	r.Groups[0].Members = append(r.Groups[0].Members, Member{Kind: MemberNodeGroup, Ref: "🇭🇰 香港"})
	r.Rules = append(r.Rules, Rule{Type: "GEOSITE", Value: "netflix", Target: Member{Kind: MemberNodeGroup, Ref: "🇭🇰 香港"}})

	members, rules := r.RenameNodeGroup("🇭🇰 香港", "🇭🇰 香港 IEPL")
	if members != 1 || rules != 1 {
		t.Fatalf("RenameNodeGroup() = (%d, %d), want (1, 1)", members, rules)
	}
	if err := r.Validate([]string{"🇭🇰 香港 IEPL"}); err != nil {
		t.Errorf("renamed routing does not validate: %v", err)
	}
}

func TestRenameNodeGroupToEmptyDropsReferences(t *testing.T) {
	r := DefaultRouting()
	r.Groups[0].Members = append(r.Groups[0].Members, Member{Kind: MemberNodeGroup, Ref: "gone"})
	r.Rules = append(r.Rules, Rule{Type: "GEOSITE", Value: "netflix", Target: Member{Kind: MemberNodeGroup, Ref: "gone"}})
	r.Final = Member{Kind: MemberNodeGroup, Ref: "gone"}

	if members, rules := r.RenameNodeGroup("gone", ""); members != 1 || rules != 2 {
		t.Fatalf("RenameNodeGroup() = (%d, %d), want (1, 2)", members, rules)
	}
	if r.Final.Kind != MemberBuiltin || r.Final.Ref != "DIRECT" {
		t.Errorf("final policy = %+v, want DIRECT", r.Final)
	}
	if err := r.Validate(nil); err != nil {
		t.Errorf("routing does not validate after the drop: %v", err)
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
