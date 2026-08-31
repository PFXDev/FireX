package clash

import (
	"fmt"
	"strings"
)

// Routing is the structured form of a mihomo policy layout: the policy groups
// an admin composes in the UI plus the ordered rule list that points at them.
// It exists so the admin console can edit routing as data instead of as YAML
// text — Render turns it back into `proxy-groups` and `rules`.
//
// Every reference between parts is by a group's bare Name, never by the name
// clients see, so renaming an emoji can never orphan a rule.
type Routing struct {
	Groups []PolicyGroup `json:"groups"`
	Rules  []Rule        `json:"rules"`
	// Final is the MATCH target: where traffic goes when no rule hit.
	Final Member `json:"final"`
}

// PolicyGroup is one admin-composed proxy-group, e.g. "AI 服务".
type PolicyGroup struct {
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Type    string `json:"type"`
	Members []Member `json:"members"`

	TestURL   string `json:"testUrl"`
	Interval  int    `json:"interval"`
	Tolerance int    `json:"tolerance"`
}

// Member is one entry inside a policy group, or the target of a rule. The kind
// decides how Ref is read, so a policy group and a node group may share a name.
type Member struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

const (
	// MemberPolicy references another policy group by name.
	MemberPolicy = "policy"
	// MemberNodeGroup references one node group by name.
	MemberNodeGroup = "node-group"
	// MemberAllGroups expands to every node group, in their configured order.
	MemberAllGroups = "all-groups"
	// MemberAllNodes expands to every node the user may use.
	MemberAllNodes = "all-nodes"
	// MemberBuiltin is a mihomo policy such as DIRECT; Ref carries it verbatim.
	MemberBuiltin = "builtin"
)

// Rule is one line of the rule list. Value is empty only for MATCH, which the
// UI never stores here — Routing.Final owns it.
type Rule struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Target    Member `json:"target"`
	NoResolve bool   `json:"noResolve"`
	Disabled  bool   `json:"disabled"`
}

// Group is one node group handed to Render: the membership has already been
// resolved to the proxy names this particular user can see.
type Group struct {
	// Name is the reference key; Display is what the client shows.
	Name      string
	Display   string
	Type      string
	TestURL   string
	Interval  int
	Tolerance int
	Members   []string
}

// GroupTypes are the mihomo proxy-group types FireX generates.
var GroupTypes = []string{"select", "url-test", "fallback", "load-balance"}

// BuiltinPolicies are the non-group rule targets mihomo understands.
var BuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}

// RuleTypes are the rule matchers the visual editor offers. The list is
// deliberately a whitelist: a typo'd matcher is a config mihomo refuses to
// load, and the admin would only find out when a client next refreshed.
var RuleTypes = []string{
	"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX",
	"GEOSITE", "GEOIP", "IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "IP-ASN",
	"SRC-IP-CIDR", "SRC-PORT", "DST-PORT", "IN-PORT", "IN-TYPE",
	"PROCESS-NAME", "PROCESS-PATH", "NETWORK", "RULE-SET", "SUB-RULE",
}

// noResolveTypes are the matchers where `no-resolve` is meaningful: it tells
// mihomo not to resolve a domain just to test an IP rule against it.
var noResolveTypes = map[string]bool{
	"GEOIP": true, "IP-CIDR": true, "IP-CIDR6": true,
	"IP-SUFFIX": true, "IP-ASN": true, "SRC-IP-CIDR": true,
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// DisplayName is the proxy-group name clients see for this policy group.
func (g PolicyGroup) DisplayName() string {
	if g.Icon != "" {
		return g.Icon + " " + g.Name
	}
	return g.Name
}

// Validate reports the first problem that would produce a config mihomo cannot
// load. nodeGroups is the set of node group names that currently exist, so a
// rule pointing at a group nobody defined is caught while the admin is still
// looking at the editor.
func (r *Routing) Validate(nodeGroups []string) error {
	if len(r.Groups) == 0 {
		return fmt.Errorf("clash: routing needs at least one policy group")
	}

	byName := make(map[string]PolicyGroup, len(r.Groups))
	displays := map[string]string{}
	for _, g := range r.Groups {
		name := strings.TrimSpace(g.Name)
		switch {
		case name == "":
			return fmt.Errorf("clash: a policy group has no name")
		case strings.Contains(name, ","), strings.Contains(g.Icon, ","):
			// Rules are comma-separated; a comma in a name splits the line.
			return fmt.Errorf("clash: policy group %q must not contain a comma", name)
		case !contains(GroupTypes, g.Type):
			return fmt.Errorf("clash: policy group %q has unknown type %q", name, g.Type)
		}
		if _, dup := byName[name]; dup {
			return fmt.Errorf("clash: duplicate policy group %q", name)
		}
		if other, dup := displays[g.DisplayName()]; dup {
			return fmt.Errorf("clash: policy groups %q and %q render to the same name %q", other, name, g.DisplayName())
		}
		byName[name] = g
		displays[g.DisplayName()] = name
	}
	for _, name := range nodeGroups {
		if other, dup := displays[name]; dup {
			return fmt.Errorf("clash: policy group %q collides with node group %q", other, name)
		}
	}

	known := map[string]bool{}
	for _, name := range nodeGroups {
		known[name] = true
	}
	for _, g := range r.Groups {
		if len(g.Members) == 0 {
			return fmt.Errorf("clash: policy group %q has no members", g.Name)
		}
		for _, m := range g.Members {
			if err := validateMember(m, byName, known, false); err != nil {
				return fmt.Errorf("clash: policy group %q: %w", g.Name, err)
			}
		}
	}
	for i, rule := range r.Rules {
		if err := validateRule(rule, byName, known); err != nil {
			return fmt.Errorf("clash: rule %d: %w", i+1, err)
		}
	}
	if err := validateMember(r.Final, byName, known, true); err != nil {
		return fmt.Errorf("clash: final policy: %w", err)
	}
	return cycleCheck(r.Groups, byName)
}

func validateMember(m Member, policies map[string]PolicyGroup, nodeGroups map[string]bool, targetOnly bool) error {
	switch m.Kind {
	case MemberPolicy:
		if _, ok := policies[m.Ref]; !ok {
			return fmt.Errorf("unknown policy group %q", m.Ref)
		}
	case MemberNodeGroup:
		if !nodeGroups[m.Ref] {
			return fmt.Errorf("unknown node group %q", m.Ref)
		}
	case MemberBuiltin:
		if !contains(BuiltinPolicies, m.Ref) {
			return fmt.Errorf("unknown built-in policy %q", m.Ref)
		}
	case MemberAllGroups, MemberAllNodes:
		if targetOnly {
			return fmt.Errorf("%q cannot be a rule target", m.Kind)
		}
	default:
		return fmt.Errorf("unknown member kind %q", m.Kind)
	}
	return nil
}

func validateRule(rule Rule, policies map[string]PolicyGroup, nodeGroups map[string]bool) error {
	if !contains(RuleTypes, rule.Type) {
		return fmt.Errorf("unknown matcher %q", rule.Type)
	}
	value := strings.TrimSpace(rule.Value)
	if value == "" {
		return fmt.Errorf("%s needs a value", rule.Type)
	}
	if strings.Contains(value, ",") {
		return fmt.Errorf("%s value %q must not contain a comma", rule.Type, value)
	}
	return validateMember(rule.Target, policies, nodeGroups, true)
}

// cycleCheck rejects a policy group that reaches itself through other groups.
// mihomo refuses to start on such a config rather than ignoring the loop.
func cycleCheck(groups []PolicyGroup, byName map[string]PolicyGroup) error {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var walk func(name string, path []string) error
	walk = func(name string, path []string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("clash: policy groups form a loop: %s → %s", strings.Join(path, " → "), name)
		}
		state[name] = visiting
		for _, m := range byName[name].Members {
			if m.Kind != MemberPolicy {
				continue
			}
			if err := walk(m.Ref, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	for _, g := range groups {
		if err := walk(g.Name, nil); err != nil {
			return err
		}
	}
	return nil
}

// RenameNodeGroup rewrites every reference to a node group after the group was
// renamed. An empty replacement means the group is gone: references are
// removed instead, rules that targeted it are dropped, and a final policy that
// pointed at it falls back to DIRECT. It returns how many members and rules
// changed, so the caller can tell the admin what their rename cost.
//
// Doing this eagerly is the point of referencing groups by name: a stored
// routing that still names a group nobody defines would fail its own
// validation the next time the editor saved it.
func (r *Routing) RenameNodeGroup(old, replacement string) (members, rules int) {
	match := func(m Member) bool { return m.Kind == MemberNodeGroup && m.Ref == old }

	for i := range r.Groups {
		kept := make([]Member, 0, len(r.Groups[i].Members))
		for _, m := range r.Groups[i].Members {
			if !match(m) {
				kept = append(kept, m)
				continue
			}
			members++
			if replacement != "" {
				m.Ref = replacement
				kept = append(kept, m)
			}
		}
		r.Groups[i].Members = kept
	}

	keptRules := make([]Rule, 0, len(r.Rules))
	for _, rule := range r.Rules {
		if !match(rule.Target) {
			keptRules = append(keptRules, rule)
			continue
		}
		rules++
		if replacement != "" {
			rule.Target.Ref = replacement
			keptRules = append(keptRules, rule)
		}
	}
	r.Rules = keptRules

	if match(r.Final) {
		rules++
		if replacement != "" {
			r.Final.Ref = replacement
		} else {
			r.Final = Member{Kind: MemberBuiltin, Ref: "DIRECT"}
		}
	}
	return members, rules
}

// compile turns the routing model into the proxy-groups and rule lines for one
// user. Policy groups come first because they are what a client's UI leads
// with; the node groups they draw from follow in their configured order.
//
// References that no longer resolve — a node group deleted after the routing
// was saved — are dropped rather than emitted, and any group left empty is
// pruned by the caller.
func (r *Routing) compile(in Input) ([]any, []any) {
	nodeGroups := make(map[string]Group, len(in.Groups))
	var groupOrder []string
	for _, g := range in.Groups {
		nodeGroups[g.Name] = g
		groupOrder = append(groupOrder, g.Name)
	}
	policies := make(map[string]PolicyGroup, len(r.Groups))
	for _, g := range r.Groups {
		policies[g.Name] = g
	}

	var nodeNames []string
	for _, n := range in.Nodes {
		if n.Entry != nil {
			nodeNames = append(nodeNames, n.Name)
		}
	}

	// resolve maps a member onto the client-visible names it stands for.
	resolve := func(m Member) []string {
		switch m.Kind {
		case MemberPolicy:
			if g, ok := policies[m.Ref]; ok {
				return []string{g.DisplayName()}
			}
		case MemberNodeGroup:
			if g, ok := nodeGroups[m.Ref]; ok {
				return []string{g.Display}
			}
		case MemberAllGroups:
			out := make([]string, 0, len(groupOrder))
			for _, name := range groupOrder {
				out = append(out, nodeGroups[name].Display)
			}
			return out
		case MemberAllNodes:
			return nodeNames
		case MemberBuiltin:
			return []string{m.Ref}
		}
		return nil
	}

	out := make([]any, 0, len(r.Groups)+len(in.Groups))
	for _, g := range r.Groups {
		members := make([]any, 0, len(g.Members))
		seen := map[string]bool{}
		for _, m := range g.Members {
			for _, name := range resolve(m) {
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				members = append(members, name)
			}
		}
		out = append(out, policyGroupNode(g, members))
	}
	for _, g := range in.Groups {
		members := make([]any, 0, len(g.Members))
		for _, name := range g.Members {
			members = append(members, name)
		}
		out = append(out, nodeGroupNode(g, members))
	}

	rules := make([]any, 0, len(r.Rules)+1)
	for _, rule := range r.Rules {
		if rule.Disabled {
			continue
		}
		target := resolve(rule.Target)
		if len(target) == 0 {
			continue
		}
		line := rule.Type + "," + strings.TrimSpace(rule.Value) + "," + target[0]
		if rule.NoResolve && noResolveTypes[rule.Type] {
			line += ",no-resolve"
		}
		rules = append(rules, line)
	}
	if final := resolve(r.Final); len(final) > 0 {
		rules = append(rules, "MATCH,"+final[0])
	} else {
		rules = append(rules, "MATCH,DIRECT")
	}
	return out, rules
}

func policyGroupNode(g PolicyGroup, members []any) *Ordered {
	node := NewOrdered()
	node.Set("name", g.DisplayName())
	node.Set("type", g.Type)
	applyProbe(node, g.Type, g.TestURL, g.Interval, g.Tolerance)
	node.Set("proxies", members)
	return node
}

func nodeGroupNode(g Group, members []any) *Ordered {
	node := NewOrdered()
	node.Set("name", g.Display)
	node.Set("type", g.Type)
	applyProbe(node, g.Type, g.TestURL, g.Interval, g.Tolerance)
	node.Set("proxies", members)
	return node
}

// applyProbe adds the latency-test fields, but only for the group types that
// read them — mihomo rejects `url` on a select group.
func applyProbe(node *Ordered, groupType, testURL string, interval, tolerance int) {
	if groupType == "select" {
		return
	}
	defaults := defaultRenderOpts()
	if testURL == "" {
		testURL = defaults.testURL
	}
	if interval <= 0 {
		interval = defaults.interval
	}
	node.Set("url", testURL)
	node.Set("interval", interval)
	if groupType == "url-test" {
		if tolerance <= 0 {
			tolerance = defaults.tolerance
		}
		node.Set("tolerance", tolerance)
	}
}
