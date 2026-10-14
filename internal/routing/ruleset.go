package routing

import (
	"fmt"
	"strings"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// RuleSetType is the rule matcher that points a policy at a rule set.
const RuleSetType = "RULE-SET"

// RuleSetInterval is how often a client re-fetches a FireX rule set, in
// seconds. It is shorter than the subscription's own refresh so a list edit
// reaches clients without them re-importing anything.
const RuleSetInterval = 6 * 60 * 60

// RuleSetURL builds the address a client fetches one rule set from. The caller
// knows the public origin and whose subscription is being rendered.
type RuleSetURL func(set model.RuleSet) string

// ruleSetExcluded are matchers a classical rule-provider refuses: a provider
// line has no target, and providers do not nest.
var ruleSetExcluded = map[string]bool{RuleSetType: true, "SUB-RULE": true, "MATCH": true}

// RuleSetLines validates a rule set's payload and returns the lines a client
// is served, with blank lines and # comments removed.
func RuleSetLines(payload string) ([]string, error) {
	var out []string
	for i, raw := range strings.Split(payload, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		matcher := strings.ToUpper(parts[0])
		switch {
		case ruleSetExcluded[matcher]:
			return nil, fmt.Errorf("第 %d 行「%s」：规则集里不能使用 %s", i+1, line, matcher)
		case !known(RuleTypes, matcher):
			return nil, fmt.Errorf("第 %d 行「%s」：未知的匹配类型「%s」", i+1, line, parts[0])
		case len(parts) < 2 || parts[1] == "":
			return nil, fmt.Errorf("第 %d 行「%s」：缺少匹配内容", i+1, line)
		case len(parts) > 3:
			return nil, fmt.Errorf("第 %d 行「%s」：规则集的一行只写「类型,内容」，不写策略名", i+1, line)
		case len(parts) == 3 && (!strings.EqualFold(parts[2], "no-resolve") || !NoResolveTypes[matcher]):
			return nil, fmt.Errorf("第 %d 行「%s」：第三段只能是 no-resolve，且只用于 IP 类规则", i+1, line)
		}
		parts[0] = matcher
		if len(parts) == 3 {
			parts[2] = "no-resolve"
		}
		out = append(out, strings.Join(parts, ","))
	}
	return out, nil
}

// validateRuleSets checks every stored rule set and returns the names a
// RULE-SET rule may point at: FireX's own sets plus the template's providers.
func validateRuleSets(db *store.DB) (map[string]bool, error) {
	var sets []model.RuleSet
	if err := db.Find(&sets).Error; err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(sets))
	for _, set := range sets {
		name := strings.TrimSpace(set.Name)
		switch {
		case name == "":
			return nil, fmt.Errorf("有一个规则集没有名称")
		case strings.Contains(name, ","):
			return nil, fmt.Errorf("规则集「%s」的名称不能包含英文逗号", name)
		}
		if _, err := RuleSetLines(set.Payload); err != nil {
			return nil, fmt.Errorf("规则集「%s」%w", name, err)
		}
		names[name] = true
	}
	external, err := clash.TemplateRuleProviders(db.GetSetting(store.SettingClashTemplate, ""))
	if err != nil {
		// A broken template is refused when it is saved; routing should not
		// become unsaveable because of one stored before that check existed.
		return names, nil
	}
	for _, name := range external {
		names[name] = true
	}
	return names, nil
}

// ruleProviders lists the FireX rule sets the emitted rules reference, in the
// order the rules first name them. Template-declared providers are left to the
// template.
func ruleProviders(db *store.DB, cells []cell, url RuleSetURL) ([]clash.RuleProvider, error) {
	var sets []model.RuleSet
	if err := db.Find(&sets).Error; err != nil {
		return nil, err
	}
	if len(sets) == 0 {
		return nil, nil
	}
	byName := make(map[string]model.RuleSet, len(sets))
	for _, set := range sets {
		byName[set.Name] = set
	}
	var out []clash.RuleProvider
	seen := map[string]bool{}
	for _, c := range cells {
		for _, rule := range c.rules {
			name := strings.TrimSpace(rule.Value)
			if rule.Disabled || rule.Type != RuleSetType || seen[name] {
				continue
			}
			set, ok := byName[name]
			if !ok {
				continue
			}
			seen[name] = true
			out = append(out, clash.RuleProvider{Name: name, URL: url(set), Interval: RuleSetInterval})
		}
	}
	return out, nil
}
