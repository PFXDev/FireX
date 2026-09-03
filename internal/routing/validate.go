package routing

import (
	"fmt"
	"strings"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// RuleTypes are the matchers the editor offers. The list is deliberately a
// whitelist: a typo'd matcher is a config mihomo refuses to load, and the admin
// would only find out when a client next refreshed.
var RuleTypes = []string{
	"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX",
	"GEOSITE", "GEOIP", "IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "IP-ASN",
	"SRC-IP-CIDR", "SRC-PORT", "DST-PORT", "IN-PORT", "IN-TYPE",
	"PROCESS-NAME", "PROCESS-PATH", "NETWORK", "RULE-SET", "SUB-RULE",
}

// NoResolveTypes are the matchers where `no-resolve` is meaningful: it tells
// mihomo not to resolve a domain just to test an IP rule against it.
var NoResolveTypes = map[string]bool{
	"GEOIP": true, "IP-CIDR": true, "IP-CIDR6": true,
	"IP-SUFFIX": true, "IP-ASN": true, "SRC-IP-CIDR": true,
}

// MemberKinds are the egress member kinds the editor offers.
var MemberKinds = []string{
	model.MemberNodeGroup, model.MemberPolicy,
	model.MemberBuiltin, model.MemberAllNodeGroups, model.MemberAllInbounds,
}

func known(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// Validate reports the first problem in the stored matrix that would produce a
// config mihomo cannot load. It reads the database rather than a request body
// so it always checks the same shape Compile will render from — the API writes
// the matrix inside a transaction and rolls back when this fails.
func Validate(db *store.DB) error {
	var policies []model.Policy
	if err := db.Order("sort_order ASC, id ASC").Find(&policies).Error; err != nil {
		return err
	}
	var groups []model.NodeGroup
	if err := db.Find(&groups).Error; err != nil {
		return err
	}

	// Both policies and node groups become proxy-groups, so their rendered
	// names share one namespace.
	displays := map[string]string{}
	for i := range groups {
		g := &groups[i]
		if strings.Contains(g.Name, ",") || strings.Contains(g.Emoji, ",") {
			return fmt.Errorf("节点组「%s」的名称或图标不能包含英文逗号", g.Name)
		}
		displays[g.DisplayName()] = "节点组 " + g.Name
	}

	finals := 0
	byName := make(map[string]model.Policy, len(policies))
	for i := range policies {
		p := &policies[i]
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "":
			return fmt.Errorf("有一个分流策略没有名称")
		case strings.Contains(name, ","), strings.Contains(p.Icon, ","):
			// Rules are comma-separated; a comma in a name splits the line.
			return fmt.Errorf("分流策略「%s」的名称或图标不能包含英文逗号", name)
		}
		if owner, dup := displays[p.DisplayName()]; dup {
			return fmt.Errorf("分流策略「%s」与%s渲染出同一个名称「%s」", name, owner, p.DisplayName())
		}
		displays[p.DisplayName()] = "分流策略 " + name
		byName[name] = *p
		if p.IsFinal && p.Enabled {
			finals++
		}
	}
	if len(policies) > 0 && finals != 1 {
		return fmt.Errorf("需要且只需要一个启用的兜底策略，当前有 %d 个", finals)
	}

	groupNames := map[string]bool{}
	for i := range groups {
		groupNames[groups[i].Name] = true
	}
	var rules []model.Rule
	if err := db.Find(&rules).Error; err != nil {
		return err
	}
	for _, rule := range rules {
		if !known(RuleTypes, rule.Type) {
			return fmt.Errorf("规则用了未知的匹配类型「%s」", rule.Type)
		}
		value := strings.TrimSpace(rule.Value)
		if value == "" {
			return fmt.Errorf("%s 规则缺少内容", rule.Type)
		}
		if strings.Contains(value, ",") {
			return fmt.Errorf("%s 规则的内容「%s」不能包含英文逗号", rule.Type, value)
		}
	}

	var profiles []model.Profile
	if err := db.Find(&profiles).Error; err != nil {
		return err
	}
	columns := []uint{model.DefaultProfileID}
	for _, p := range profiles {
		columns = append(columns, p.ID)
	}
	for _, profileID := range columns {
		cells, err := loadCells(db, profileID)
		if err != nil {
			return err
		}
		if err := validateColumn(cells, groupNames, profileID); err != nil {
			return err
		}
	}
	return nil
}

func validateColumn(cells []cell, groupNames map[string]bool, profileID uint) error {
	where := "默认出口"
	if profileID != model.DefaultProfileID {
		where = fmt.Sprintf("方案 #%d 的出口", profileID)
	}
	visible := make(map[string]cell, len(cells))
	for _, c := range cells {
		visible[c.policy.Name] = c
	}
	for _, c := range cells {
		if !known([]string{model.GroupTypeSelect, model.GroupTypeURLTest, model.GroupTypeFallback, model.GroupTypeLoadBalance}, c.egress.Type) {
			return fmt.Errorf("%s:「%s」的选择方式「%s」无效", where, c.policy.Name, c.egress.Type)
		}
		for _, m := range c.members {
			switch m.Kind {
			case model.MemberNodeGroup:
				if !groupNames[m.Ref] {
					return fmt.Errorf("%s:「%s」引用了不存在的节点组「%s」", where, c.policy.Name, m.Ref)
				}
			case model.MemberPolicy:
				if _, ok := visible[m.Ref]; !ok {
					// A reference to a policy this column hides is dropped at
					// render time, not an error — hiding one is how a tier opts
					// out. Only a name nobody defines is worth refusing.
					continue
				}
			case model.MemberBuiltin:
				if !known(model.BuiltinPolicies, m.Ref) {
					return fmt.Errorf("%s:「%s」引用了未知的内置策略「%s」", where, c.policy.Name, m.Ref)
				}
			case model.MemberAllNodeGroups, model.MemberAllInbounds:
			default:
				return fmt.Errorf("%s:「%s」有一个未知的成员类型「%s」", where, c.policy.Name, m.Kind)
			}
		}
	}
	return cycleCheck(cells, visible, where)
}

// cycleCheck rejects a policy that reaches itself through other policies.
// mihomo refuses to start on such a config rather than ignoring the loop.
func cycleCheck(cells []cell, visible map[string]cell, where string) error {
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
			return fmt.Errorf("%s:分流策略成环 %s → %s", where, strings.Join(path, " → "), name)
		}
		state[name] = visiting
		for _, m := range visible[name].members {
			if m.Kind != model.MemberPolicy {
				continue
			}
			if _, ok := visible[m.Ref]; !ok {
				continue
			}
			if err := walk(m.Ref, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	for _, c := range cells {
		if err := walk(c.policy.Name, nil); err != nil {
			return err
		}
	}
	return nil
}
