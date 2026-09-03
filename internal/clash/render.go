// Package clash renders a mihomo (Clash.Meta) config: it takes an
// admin-editable YAML template for the base settings and substitutes in the
// proxies, proxy-groups and rules that another package has already resolved for
// one particular user.
//
// Nothing here knows about profiles, policies or node groups — by the time
// Input arrives, every member list is a plain list of names.
package clash

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proxy is one client-visible proxy: the name it goes by and its mihomo
// mapping.
type Proxy struct {
	Name  string
	Entry *Ordered
}

// Group is one proxy-group to emit. Name is what clients see, and Members are
// the proxy or group names it references, already resolved.
type Group struct {
	Name      string
	Type      string
	TestURL   string
	Interval  int
	Tolerance int
	Members   []string
}

type Input struct {
	Proxies []Proxy
	// Groups is the whole proxy-group list in emit order: the policies a client
	// leads with first, then the node groups they draw from.
	Groups []Group
	// Rules are complete mihomo rule lines, the trailing MATCH included.
	Rules []string
}

// GroupTypes are the mihomo proxy-group types FireX generates.
var GroupTypes = []string{"select", "url-test", "fallback", "load-balance"}

// Probe defaults for the group types that latency-test.
const (
	DefaultTestURL   = "https://www.gstatic.com/generate_204"
	DefaultInterval  = 300
	DefaultTolerance = 50
)

// Render substitutes the user's config into the template and returns YAML.
func Render(template string, in Input) (string, error) {
	if strings.TrimSpace(template) == "" {
		template = DefaultTemplate
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(template), &doc); err != nil {
		return "", fmt.Errorf("clash: parse template: %w", err)
	}
	if len(doc.Content) == 0 {
		return "", fmt.Errorf("clash: empty template")
	}
	value, err := nodeToValue(doc.Content[0])
	if err != nil {
		return "", fmt.Errorf("clash: parse template: %w", err)
	}
	root, ok := value.(*Ordered)
	if !ok {
		return "", fmt.Errorf("clash: template root must be a mapping")
	}
	// Templates carried over from the token era may still hold a `firex:` block
	// that tuned the generated region groups. It means nothing now and mihomo
	// would reject the unknown key, so drop it on the way through.
	root.delete("firex")

	proxies := make([]any, 0, len(in.Proxies))
	for _, p := range in.Proxies {
		if p.Entry != nil {
			proxies = append(proxies, p.Entry)
		}
	}
	root.Set("proxies", proxies)

	groups := make([]any, 0, len(in.Groups))
	for _, g := range in.Groups {
		groups = append(groups, groupNode(g))
	}
	groups, _ = pruneGroups(groups)
	root.Set("proxy-groups", groups)

	rules := make([]any, 0, len(in.Rules))
	for _, rule := range in.Rules {
		rules = append(rules, rule)
	}
	root.Set("rules", rules)
	rewriteRules(root, surviving(groups), firstGroupName(groups))

	out, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("clash: encode: %w", err)
	}
	return string(out), nil
}

func groupNode(g Group) *Ordered {
	node := NewOrdered()
	node.Set("name", g.Name)
	node.Set("type", g.Type)
	applyProbe(node, g.Type, g.TestURL, g.Interval, g.Tolerance)
	members := make([]any, 0, len(g.Members))
	for _, m := range g.Members {
		members = append(members, m)
	}
	node.Set("proxies", members)
	return node
}

// applyProbe adds the latency-test fields, but only for the group types that
// read them — mihomo rejects `url` on a select group.
func applyProbe(node *Ordered, groupType, testURL string, interval, tolerance int) {
	if groupType == "select" {
		return
	}
	if testURL == "" {
		testURL = DefaultTestURL
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	node.Set("url", testURL)
	node.Set("interval", interval)
	if groupType == "url-test" {
		if tolerance <= 0 {
			tolerance = DefaultTolerance
		}
		node.Set("tolerance", tolerance)
	}
}

// pruneGroups removes groups left with no members — mihomo rejects an empty
// proxy-group — then drops references to the removed names, which can empty
// another group, so it repeats until the set is stable.
func pruneGroups(groups []any) (kept []any, dropped map[string]bool) {
	dropped = map[string]bool{}
	current := groups
	for {
		var survivors []any
		newlyDropped := false
		for _, item := range current {
			g, ok := item.(*Ordered)
			if !ok {
				survivors = append(survivors, item)
				continue
			}
			list, _ := g.Get("proxies")
			members, _ := list.([]any)
			if len(members) == 0 {
				if name, ok := groupName(g); ok {
					dropped[name] = true
				}
				newlyDropped = true
				continue
			}
			survivors = append(survivors, g)
		}
		if !newlyDropped {
			return survivors, dropped
		}
		for _, item := range survivors {
			g, ok := item.(*Ordered)
			if !ok {
				continue
			}
			list, _ := g.Get("proxies")
			members, _ := list.([]any)
			filtered := make([]any, 0, len(members))
			for _, m := range members {
				if s, ok := m.(string); ok && dropped[s] {
					continue
				}
				filtered = append(filtered, m)
			}
			g.Set("proxies", filtered)
		}
		current = survivors
	}
}

// builtinTargets are the rule targets that need no proxy-group behind them.
var builtinTargets = map[string]bool{
	"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
	"PASS": true, "GLOBAL": true, "COMPATIBLE": true,
}

func surviving(groups []any) map[string]bool {
	out := map[string]bool{}
	for _, item := range groups {
		if g, ok := item.(*Ordered); ok {
			if name, ok := groupName(g); ok {
				out[name] = true
			}
		}
	}
	return out
}

// rewriteRules repoints every rule whose target no longer exists — a group that
// pruning removed, or one the caller named but never emitted. mihomo refuses to
// load a config that references a missing group, and an expired user with no
// proxies left still has to receive something loadable.
func rewriteRules(root *Ordered, known map[string]bool, fallbackGroup string) {
	raw, ok := root.Get("rules")
	if !ok {
		return
	}
	rules, ok := raw.([]any)
	if !ok {
		return
	}
	replacement := fallbackGroup
	if replacement == "" {
		replacement = "DIRECT"
	}
	out := make([]any, 0, len(rules))
	for _, item := range rules {
		rule, ok := item.(string)
		if !ok {
			out = append(out, item)
			continue
		}
		parts := strings.Split(rule, ",")
		idx := targetIndex(parts)
		if idx >= 0 {
			target := strings.TrimSpace(parts[idx])
			if !known[target] && !builtinTargets[strings.ToUpper(target)] {
				parts[idx] = replacement
				rule = strings.Join(parts, ",")
			}
		}
		out = append(out, rule)
	}
	root.Set("rules", out)
}

// targetIndex locates the policy field of a Clash rule line, which is the
// second field for MATCH and the third otherwise.
func targetIndex(parts []string) int {
	if len(parts) < 2 {
		return -1
	}
	switch strings.ToUpper(strings.TrimSpace(parts[0])) {
	case "MATCH", "FINAL":
		return 1
	}
	if len(parts) < 3 {
		return -1
	}
	return 2
}

func groupName(g *Ordered) (string, bool) {
	raw, ok := g.Get("name")
	if !ok {
		return "", false
	}
	name, ok := raw.(string)
	return name, ok
}

func firstGroupName(groups []any) string {
	for _, item := range groups {
		if g, ok := item.(*Ordered); ok {
			if name, ok := groupName(g); ok {
				return name
			}
		}
	}
	return ""
}

func (o *Ordered) delete(key string) {
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			o.vals = append(o.vals[:i], o.vals[i+1:]...)
			return
		}
	}
}

// nodeToValue converts a parsed YAML tree into Go values, keeping mappings in
// document order so the rendered config preserves the template's layout.
func nodeToValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToValue(n.Content[0])
	case yaml.MappingNode:
		out := NewOrdered()
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			val, err := nodeToValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out.Set(key, val)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			val, err := nodeToValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return out, nil
	case yaml.AliasNode:
		return nodeToValue(n.Alias)
	default:
		var scalar any
		if err := n.Decode(&scalar); err != nil {
			return nil, err
		}
		return scalar, nil
	}
}
