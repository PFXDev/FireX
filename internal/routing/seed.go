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

type seedRuleSet struct {
	name    string
	remark  string
	payload string
}

func policyRef(name string) seedMember  { return seedMember{model.MemberPolicy, name} }
func builtinRef(name string) seedMember { return seedMember{model.MemberBuiltin, name} }

var (
	allGroupsRef   = seedMember{kind: model.MemberAllNodeGroups}
	allInboundsRef = seedMember{kind: model.MemberAllInbounds}
)

// Preset is a complete policy layout an admin can start from: the rows of the
// matrix and their default column. It knows nothing about profiles, so
// applying one never changes which inbounds anybody holds.
type Preset struct {
	Key         string
	Name        string
	Description string
	policies    []seedPolicy
	// ruleSets are created only when no set of the same name exists, so
	// reapplying a preset never discards an edited list.
	ruleSets []seedRuleSet
}

// Presets lists the built-in layouts, the stock one first. Rules lean on the
// built-in GEOSITE/GEOIP databases so a client needs no third-party
// rule-provider to come up; the only rule sets are ones FireX serves itself.
//
// One order serves both rule precedence and the client's group list, so the
// manual switches lead, ad blocking comes before anything that could send the
// same request somewhere else, the per-service rows sit in the middle, and the
// broad geolocation and CN rules close as catch-alls. The mainland row ends on
// a resolving GEOIP,cn: a domain no list knows is looked up (through the
// proxied resolver) and goes direct if it lands in China, so every mainland
// address bypasses the proxy, not just the ones a list happens to name.
var Presets = []Preset{
	{
		Key:         "standard",
		Name:        "标准",
		Description: "独立分流 AI、电报、媒体、谷歌和 Steam；海外大流量下载用可编辑的规则集单独选代理，游戏 CDN 默认直连。",
		policies:    standardPolicies,
		ruleSets:    []seedRuleSet{bulkDownloadSet},
	},
	{
		Key:         "lite",
		Name:        "精简",
		Description: "只保留节点选择、OpenAI、Claude、国外网站、直连和兜底，适合不需要细分的方案。",
		policies:    litePolicies,
	},
}

// PresetByKey finds a built-in layout.
func PresetByKey(key string) (Preset, bool) {
	for _, p := range Presets {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}

var (
	selectNode = seedPolicy{
		name: "节点选择", icon: "🚀",
		members: []seedMember{policyRef("自动选择"), allGroupsRef, allInboundsRef, builtinRef("DIRECT")},
	}
	autoSelect = seedPolicy{
		name: "自动选择", icon: "♻️",
		members: []seedMember{allInboundsRef},
	}
	// OpenAI and Anthropic both refuse some regions outright and flag sessions
	// whose exit keeps changing, so each gets its own row that offers node
	// groups first and is never latency-switched.
	openAI = seedPolicy{
		name: "OpenAI", icon: "🤖",
		members: []seedMember{allGroupsRef, policyRef("节点选择")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "openai"}},
	}
	claude = seedPolicy{
		name: "Claude", icon: "✳️",
		members: []seedMember{allGroupsRef, policyRef("节点选择")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "anthropic"}},
	}
	// Keep this after the AI and media rows: Google's list also includes
	// Gemini and YouTube, which should retain their dedicated egresses.
	googleServices = seedPolicy{
		name: "谷歌服务", icon: "🔎",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "google"},
			{matcher: "GEOIP", value: "google", noResolve: true},
		},
	}
	// Large downloads from blocked or slow overseas sources get their own
	// switch, so they can ride a high-bandwidth or cheap exit. The host list
	// is a rule set rather than rows here: it changes far more often than the
	// layout and must survive a preset swap. It precedes OpenAI, AI and
	// Google because their lists overlap it.
	bulkDownloads = seedPolicy{
		name: "大流量下载", icon: "📥",
		members: []seedMember{policyRef("节点选择"), allGroupsRef},
		rules:   []seedRule{{matcher: RuleSetType, value: bulkDownloadSet.name}},
	}
	// Upstream GEOSITE lists come first; explicit hosts only fill what no
	// list scopes narrowly enough. GEOSITE,github would also take the main
	// site, Copilot and npm.
	bulkDownloadSet = seedRuleSet{
		name:   "大流量下载",
		remark: "被墙或海外直连很慢的大文件下载源",
		payload: `# Hugging Face 模型与数据集
GEOSITE,huggingface
# GitHub Release、Raw 文件与源码包
DOMAIN-SUFFIX,githubusercontent.com
DOMAIN,codeload.github.com
# Google Drive 下载
DOMAIN,drive.usercontent.google.com
DOMAIN-REGEX,^doc-[^.]+-docs\.googleusercontent\.com$
# MEGA
GEOSITE,mega
# ChatGPT 客户端安装包
DOMAIN,persistent.oaistatic.com
# Debian / Ubuntu 官方 APT 源，Arch 官方源与 AUR
GEOSITE,debian
GEOSITE,ubuntu
GEOSITE,archlinux
`,
	}
	// Game CDNs have usable domestic mirrors and should not consume the
	// overseas download exit. Keep them ahead of Steam's storefront rules.
	gameDownloads = seedPolicy{
		name: "游戏下载", icon: "🕹️",
		members: []seedMember{builtinRef("DIRECT"), policyRef("节点选择"), allGroupsRef},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "category-game-platforms-download"},
			{matcher: "DOMAIN-SUFFIX", value: "steamcontent.com"},
			{matcher: "DOMAIN-SUFFIX", value: "steamcdn-a.akamaihd.net"},
		},
	}
	steam = seedPolicy{
		name: "Steam", icon: "🎮",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef, builtinRef("DIRECT")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "steam"}},
	}
	// DIRECT leads because it is safe on every network: it races both
	// families for TCP and sends UDP over IPv4. DIRECT-V6 is offered for
	// clients with good IPv6, but its UDP never falls back to IPv4, which
	// breaks QUIC to dual-stack sites on an IPv4-only network.
	globalDirect = seedPolicy{
		name: "全球直连", icon: "🎯",
		members: []seedMember{builtinRef("DIRECT"), builtinRef(model.BuiltinDirectV6), policyRef("节点选择")},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "private"},
			{matcher: "GEOSITE", value: "cn"},
			{matcher: "GEOIP", value: "private", noResolve: true},
			{matcher: "GEOIP", value: "cn"},
		},
	}
	finalMatch = seedPolicy{
		name: "漏网之鱼", icon: "🐟", final: true,
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), builtinRef("DIRECT")},
	}
)

var standardPolicies = []seedPolicy{
	selectNode,
	autoSelect,
	{
		name: "广告拦截", icon: "🛑",
		members: []seedMember{builtinRef("REJECT"), builtinRef("DIRECT")},
		rules:   []seedRule{{matcher: "GEOSITE", value: "category-ads-all"}},
	},
	bulkDownloads,
	openAI,
	claude,
	{
		name: "AI 服务", icon: "✨",
		members: []seedMember{policyRef("节点选择"), allGroupsRef},
		rules:   []seedRule{{matcher: "GEOSITE", value: "category-ai-!cn"}},
	},
	{
		name: "电报消息", icon: "📱",
		members: []seedMember{policyRef("节点选择"), allGroupsRef},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "telegram"},
			{matcher: "GEOIP", value: "telegram", noResolve: true},
		},
	},
	{
		name: "国外媒体", icon: "📺",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "youtube"},
			{matcher: "GEOSITE", value: "netflix"},
			{matcher: "GEOSITE", value: "disney"},
			{matcher: "GEOSITE", value: "hbo"},
			{matcher: "GEOSITE", value: "primevideo"},
			{matcher: "GEOSITE", value: "spotify"},
			{matcher: "GEOSITE", value: "tiktok"},
			{matcher: "GEOIP", value: "netflix", noResolve: true},
		},
	},
	googleServices,
	gameDownloads,
	steam,
	// Apple serves the mainland well from its own CDN; the row exists so a
	// user who wants the foreign storefront can flip it in one place.
	{
		name: "苹果服务", icon: "🍎",
		members: []seedMember{builtinRef("DIRECT"), policyRef("节点选择"), allGroupsRef},
		rules:   []seedRule{{matcher: "GEOSITE", value: "apple"}},
	},
	{
		name: "国外网站", icon: "🌐",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef},
		rules:   []seedRule{{matcher: "GEOSITE", value: "geolocation-!cn"}},
	},
	globalDirect,
	finalMatch,
}

var litePolicies = []seedPolicy{
	selectNode,
	autoSelect,
	openAI,
	claude,
	{
		name: "国外网站", icon: "🌐",
		members: []seedMember{policyRef("节点选择"), policyRef("自动选择"), allGroupsRef},
		rules: []seedRule{
			{matcher: "GEOSITE", value: "telegram"},
			{matcher: "GEOIP", value: "telegram", noResolve: true},
			{matcher: "GEOSITE", value: "geolocation-!cn"},
		},
	},
	globalDirect,
	finalMatch,
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

	if err := install(db, Presets[0]); err != nil {
		return err
	}
	return ensureDefaultProfile(db)
}

// ApplyPreset replaces every policy with a built-in layout. Profiles and their
// whitelists are untouched; a profile's override survives when the preset has
// a policy of the same name, minus any member pointing at a policy the preset
// lacks. Rule sets are kept, and the preset's own are added only where none of
// that name exists. Callers run it inside a transaction and Validate before
// committing.
func ApplyPreset(db *store.DB, preset Preset) error {
	var oldPolicies []model.Policy
	if err := db.Find(&oldPolicies).Error; err != nil {
		return err
	}
	nameByID := make(map[uint]string, len(oldPolicies))
	for _, p := range oldPolicies {
		nameByID[p.ID] = p.Name
	}
	var overrides []model.Egress
	if err := db.Where("profile_id <> ?", model.DefaultProfileID).Find(&overrides).Error; err != nil {
		return err
	}
	overrideMembers := map[uint][]model.EgressMember{}
	if len(overrides) > 0 {
		ids := make([]uint, 0, len(overrides))
		for _, e := range overrides {
			ids = append(ids, e.ID)
		}
		var rows []model.EgressMember
		if err := db.Where("egress_id IN ?", ids).Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
			return err
		}
		for _, m := range rows {
			overrideMembers[m.EgressID] = append(overrideMembers[m.EgressID], m)
		}
	}

	for _, table := range []any{&model.EgressMember{}, &model.Egress{}, &model.Rule{}, &model.Policy{}} {
		if err := db.Where("1 = 1").Delete(table).Error; err != nil {
			return err
		}
	}
	if err := install(db, preset); err != nil {
		return err
	}

	var created []model.Policy
	if err := db.Find(&created).Error; err != nil {
		return err
	}
	idByName := make(map[string]uint, len(created))
	for _, p := range created {
		idByName[p.Name] = p.ID
	}
	for _, old := range overrides {
		policyID, ok := idByName[nameByID[old.PolicyID]]
		if !ok {
			continue
		}
		var kept []model.EgressMember
		for _, m := range overrideMembers[old.ID] {
			if m.Kind == model.MemberPolicy {
				if _, ok := idByName[m.Ref]; !ok {
					continue
				}
			}
			kept = append(kept, m)
		}
		// An override emptied by the swap would render as nothing at all;
		// letting the column fall back to the preset's default is closer to
		// what the operator meant.
		if len(kept) == 0 && !old.Hidden {
			continue
		}
		egress := old
		egress.ID = 0
		egress.PolicyID = policyID
		if err := db.Create(&egress).Error; err != nil {
			return err
		}
		for j, m := range kept {
			row := model.EgressMember{EgressID: egress.ID, SortOrder: j, Kind: m.Kind, Ref: m.Ref}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// install writes a layout's policies, rules and default column into an empty
// matrix, plus whichever of its rule sets do not exist yet.
func install(db *store.DB, preset Preset) error {
	for _, spec := range preset.ruleSets {
		var count int64
		if err := db.Model(&model.RuleSet{}).Where("name = ?", spec.name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&model.RuleSet{Name: spec.name, Payload: spec.payload, Remark: spec.remark}).Error; err != nil {
			return err
		}
	}
	for i, spec := range preset.policies {
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
	return nil
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
