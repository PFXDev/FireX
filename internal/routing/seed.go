package routing

import (
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// DefaultProfileName is the profile a fresh install starts with: it takes every
// node group, so an operator who never wants tiers never has to think about
// profiles at all.
const DefaultProfileName = "默认"

type seedMember struct {
	kind string
	ref  string
}

type seedRule struct {
	matcher   string
	value     string
	noResolve bool
}

type seedPolicy struct {
	name    string
	icon    string
	final   bool
	members []seedMember
	rules   []seedRule
}

func policyRef(name string) seedMember  { return seedMember{model.MemberPolicy, name} }
func builtinRef(name string) seedMember { return seedMember{model.MemberBuiltin, name} }

var (
	allGroupsRef   = seedMember{kind: model.MemberAllNodeGroups}
	allInboundsRef = seedMember{kind: model.MemberAllInbounds}
)

// stockPolicies is the layout a fresh install starts from. Rules lean on the
// built-in GEOSITE/GEOIP databases so a client needs no external rule-provider
// fetch to come up.
//
// One order serves both rule precedence and the client's group list, so the
// manual switches lead, ad blocking comes before anything that could send the
// same request somewhere else, and the broad geolocation and CN rules sit at the
// end where they act as catch-alls.
var stockPolicies = []seedPolicy{
	{
		name: "节点选择", icon: "🚀",
		members: []seedMember{policyRef("自动选择"), allGroupsRef, builtinRef("DIRECT"), allInboundsRef},
	},
	{
		name: "自动选择", icon: "♻️",
		members: []seedMember{allInboundsRef},
	},
	{
		name: "广告拦截", icon: "🛑",
		members: []seedMember{builtinRef("REJECT"), builtinRef("DIRECT")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "category-ads-all"}},
	},
	{
		name: "AI 服务", icon: "🤖",
		members: []seedMember{policyRef("节点选择"), allGroupsRef, builtinRef("DIRECT")},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "openai"},
			{matcher: "GEOSITE", value: "anthropic"},
			{matcher: "GEOSITE", value: "google-gemini"},
		},
	},
	{
		name: "电报消息", icon: "📱",
		members: []seedMember{policyRef("节点选择"), allGroupsRef, builtinRef("DIRECT")},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "telegram"},
			{matcher: "GEOIP", value: "telegram", noResolve: true},
		},
	},
	{
		name: "国外媒体", icon: "📺",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef, builtinRef("DIRECT")},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "youtube"},
			{matcher: "GEOSITE", value: "netflix"},
			{matcher: "GEOSITE", value: "disney"},
			{matcher: "GEOSITE", value: "spotify"},
		},
	},
	{
		name: "苹果服务", icon: "🍎",
		members: []seedMember{builtinRef("DIRECT"), policyRef("节点选择")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "apple-cn"}},
	},
	{
		name: "微软服务", icon: "Ⓜ️",
		members: []seedMember{builtinRef("DIRECT"), policyRef("节点选择")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "microsoft@cn"}},
	},
	{
		name: "国外网站", icon: "🌐",
		members: []seedMember{policyRef("节点选择"), allGroupsRef, builtinRef("DIRECT")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "geolocation-!cn"}},
	},
	{
		name: "全球直连", icon: "🎯",
		members: []seedMember{builtinRef("DIRECT"), policyRef("节点选择")},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "private"},
			{matcher: "GEOSITE", value: "bilibili"},
			{matcher: "GEOSITE", value: "cn"},
			{matcher: "GEOIP", value: "private", noResolve: true},
			{matcher: "GEOIP", value: "cn"},
		},
	},
	{
		name: "漏网之鱼", icon: "🐟", final: true,
		members: []seedMember{policyRef("节点选择"), builtinRef("DIRECT"), policyRef("自动选择")},
	},
}

// Seed installs the stock matrix on a database that has none. It is called on
// every start and does nothing once policies exist, so an operator who deletes
// one does not get it back on the next restart.
func Seed(db *store.DB) error {
	var policies int64
	if err := db.Model(&model.Policy{}).Count(&policies).Error; err != nil {
		return err
	}
	if policies > 0 {
		return ensureDefaultProfile(db)
	}

	for i, spec := range stockPolicies {
		policy := model.Policy{
			Name: spec.name, Icon: spec.icon, IsFinal: spec.final,
			SortOrder: (i + 1) * 10, Enabled: true,
		}
		if err := db.Create(&policy).Error; err != nil {
			return err
		}
		for j, rule := range spec.rules {
			row := model.Rule{
				PolicyID: policy.ID, SortOrder: j,
				Type: rule.matcher, Value: rule.value, NoResolve: rule.noResolve,
			}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
		}
		egress := model.Egress{
			PolicyID: policy.ID, ProfileID: model.DefaultProfileID,
			Type: model.GroupTypeSelect, Interval: 300, Tolerance: 50,
		}
		if spec.name == "自动选择" {
			egress.Type = model.GroupTypeURLTest
		}
		if err := db.Create(&egress).Error; err != nil {
			return err
		}
		for j, member := range spec.members {
			row := model.EgressMember{EgressID: egress.ID, SortOrder: j, Kind: member.kind, Ref: member.ref}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return ensureDefaultProfile(db)
}

func ensureDefaultProfile(db *store.DB) error {
	var count int64
	if err := db.Model(&model.Profile{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Create(&model.Profile{
		Name: DefaultProfileName, AllGroups: true, SortOrder: 100, Enabled: true,
		Remark: "包含全部节点组;要分层就新建方案并挑选节点组",
	}).Error
}
