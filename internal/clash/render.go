// Package clash renders a per-user mihomo (Clash.Meta) config from an
// admin-editable YAML template plus the set of nodes the user may use.
package clash

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node is one subscription entry: the rendered proxy plus the metadata the
// template's group tokens select on.
type Node struct {
	Name   string
	Region string
	Tags   []string
	Entry  *Ordered
}

type Input struct {
	Nodes []Node
}

// Group-list tokens the template may use inside proxy-groups.
const (
	tokenAll          = "<ALL>"
	tokenRegionGroups = "<REGION_GROUPS>"
	prefixRegion      = "<REGION:"
	prefixTag         = "<TAG:"
	prefixFilter      = "<FILTER:"
)

type renderOpts struct {
	regionGroupType string
	testURL         string
	interval        int
	tolerance       int
}

func defaultRenderOpts() renderOpts {
	return renderOpts{
		regionGroupType: "url-test",
		testURL:         "https://www.gstatic.com/generate_204",
		interval:        300,
		tolerance:       50,
	}
}

// Render substitutes the user's nodes into the template and returns YAML.
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

	opts := extractOpts(root)
	names, regions := indexNodes(in.Nodes)

	proxies := make([]any, 0, len(in.Nodes))
	for _, n := range in.Nodes {
		if n.Entry != nil {
			proxies = append(proxies, n.Entry)
		}
	}
	root.Set("proxies", proxies)

	groups, err := expandGroups(root, in.Nodes, names, regions, opts)
	if err != nil {
		return "", err
	}
	groups, dropped := pruneGroups(groups)
	root.Set("proxy-groups", groups)
	rewriteRules(root, dropped, firstGroupName(groups))

	out, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("clash: encode: %w", err)
	}
	return string(out), nil
}

// extractOpts consumes the optional top-level `firex` block, which tunes the
// generated region groups, and removes it so it never reaches the client.
func extractOpts(root *Ordered) renderOpts {
	opts := defaultRenderOpts()
	raw, ok := root.Get("firex")
	if !ok {
		return opts
	}
	root.delete("firex")
	cfg, ok := raw.(*Ordered)
	if !ok {
		return opts
	}
	if v, ok := cfg.Get("region-group-type"); ok {
		if s, ok := v.(string); ok && s != "" {
			opts.regionGroupType = s
		}
	}
	if v, ok := cfg.Get("test-url"); ok {
		if s, ok := v.(string); ok && s != "" {
			opts.testURL = s
		}
	}
	if v, ok := cfg.Get("interval"); ok {
		if n, ok := toInt(v); ok && n > 0 {
			opts.interval = n
		}
	}
	if v, ok := cfg.Get("tolerance"); ok {
		if n, ok := toInt(v); ok && n > 0 {
			opts.tolerance = n
		}
	}
	return opts
}

// indexNodes returns every renderable proxy name and the region order in which
// region groups should be generated.
func indexNodes(nodes []Node) (names []string, regions []string) {
	seenRegion := map[string]bool{}
	for _, n := range nodes {
		if n.Entry == nil {
			continue
		}
		names = append(names, n.Name)
		if n.Region != "" && !seenRegion[n.Region] {
			seenRegion[n.Region] = true
			regions = append(regions, n.Region)
		}
	}
	return names, regions
}

func expandGroups(root *Ordered, nodes []Node, allNames, regions []string, opts renderOpts) ([]any, error) {
	raw, ok := root.Get("proxy-groups")
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("clash: proxy-groups must be a list")
	}
	regionGroupNames := regions

	out := make([]any, 0, len(items)+len(regions))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == tokenRegionGroups {
			for _, region := range regions {
				out = append(out, regionGroup(region, nodesInRegion(nodes, region), opts))
			}
			continue
		}
		group, ok := item.(*Ordered)
		if !ok {
			out = append(out, item)
			continue
		}
		listRaw, has := group.Get("proxies")
		if !has {
			out = append(out, group)
			continue
		}
		list, ok := listRaw.([]any)
		if !ok {
			out = append(out, group)
			continue
		}
		expanded, err := expandList(list, nodes, allNames, regionGroupNames)
		if err != nil {
			return nil, err
		}
		group.Set("proxies", expanded)
		out = append(out, group)
	}
	return out, nil
}

func expandList(list []any, nodes []Node, allNames, regionGroupNames []string) ([]any, error) {
	seen := map[string]bool{}
	out := make([]any, 0, len(list))
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			out = append(out, item)
			continue
		}
		token := strings.TrimSpace(s)
		switch {
		case token == tokenAll:
			for _, n := range allNames {
				add(n)
			}
		case token == tokenRegionGroups:
			for _, n := range regionGroupNames {
				add(n)
			}
		case strings.HasPrefix(token, prefixRegion) && strings.HasSuffix(token, ">"):
			region := token[len(prefixRegion) : len(token)-1]
			for _, n := range nodesInRegion(nodes, region) {
				add(n)
			}
		case strings.HasPrefix(token, prefixTag) && strings.HasSuffix(token, ">"):
			tag := token[len(prefixTag) : len(token)-1]
			for _, n := range nodesWithTag(nodes, tag) {
				add(n)
			}
		case strings.HasPrefix(token, prefixFilter) && strings.HasSuffix(token, ">"):
			expr := token[len(prefixFilter) : len(token)-1]
			re, err := regexp.Compile(expr)
			if err != nil {
				return nil, fmt.Errorf("clash: bad <FILTER:%s>: %w", expr, err)
			}
			for _, n := range allNames {
				if re.MatchString(n) {
					add(n)
				}
			}
		default:
			add(token)
		}
	}
	return out, nil
}

func regionGroup(region string, members []string, opts renderOpts) *Ordered {
	g := NewOrdered()
	g.Set("name", region)
	g.Set("type", opts.regionGroupType)
	if opts.regionGroupType == "url-test" || opts.regionGroupType == "fallback" || opts.regionGroupType == "load-balance" {
		g.Set("url", opts.testURL)
		g.Set("interval", opts.interval)
	}
	if opts.regionGroupType == "url-test" {
		g.Set("tolerance", opts.tolerance)
	}
	members_ := make([]any, 0, len(members))
	for _, m := range members {
		members_ = append(members_, m)
	}
	g.Set("proxies", members_)
	return g
}

func nodesInRegion(nodes []Node, region string) []string {
	var out []string
	for _, n := range nodes {
		if n.Entry != nil && n.Region == region {
			out = append(out, n.Name)
		}
	}
	return out
}

func nodesWithTag(nodes []Node, tag string) []string {
	var out []string
	for _, n := range nodes {
		if n.Entry == nil {
			continue
		}
		for _, t := range n.Tags {
			if t == tag {
				out = append(out, n.Name)
				break
			}
		}
	}
	return out
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

// rewriteRules repoints rules at a group that pruning removed. Built-in
// targets (DIRECT/REJECT/…) are never rewritten.
func rewriteRules(root *Ordered, dropped map[string]bool, fallbackGroup string) {
	if len(dropped) == 0 {
		return
	}
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
			if dropped[target] {
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

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
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
