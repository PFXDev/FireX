package clash

// DefaultRouting is the policy layout a fresh install starts from: the same
// groups and rules DefaultTemplate carries, expressed as data so the visual
// editor can open them. Rules lean on the built-in GEOSITE/GEOIP databases so
// a client needs no external rule-provider fetch to come up.
func DefaultRouting() *Routing {
	policy := func(ref string) Member { return Member{Kind: MemberPolicy, Ref: ref} }
	builtin := func(ref string) Member { return Member{Kind: MemberBuiltin, Ref: ref} }
	allGroups := Member{Kind: MemberAllGroups}
	allNodes := Member{Kind: MemberAllNodes}

	rule := func(matcher, value, target string) Rule {
		return Rule{Type: matcher, Value: value, Target: policy(target)}
	}
	ipRule := func(matcher, value, target string, noResolve bool) Rule {
		return Rule{Type: matcher, Value: value, Target: policy(target), NoResolve: noResolve}
	}

	return &Routing{
		Groups: []PolicyGroup{
			{
				Name: "节点选择", Icon: "🚀", Type: "select",
				Members: []Member{policy("自动选择"), allGroups, builtin("DIRECT"), allNodes},
			},
			{
				Name: "自动选择", Icon: "♻️", Type: "url-test",
				Members: []Member{allNodes},
			},
			{
				Name: "国外媒体", Icon: "📺", Type: "select",
				Members: []Member{policy("节点选择"), policy("自动选择"), allGroups, builtin("DIRECT")},
			},
			{
				Name: "AI 服务", Icon: "🤖", Type: "select",
				Members: []Member{policy("节点选择"), allGroups, builtin("DIRECT")},
			},
			{
				Name: "电报消息", Icon: "📱", Type: "select",
				Members: []Member{policy("节点选择"), allGroups, builtin("DIRECT")},
			},
			{
				Name: "微软服务", Icon: "Ⓜ️", Type: "select",
				Members: []Member{builtin("DIRECT"), policy("节点选择")},
			},
			{
				Name: "苹果服务", Icon: "🍎", Type: "select",
				Members: []Member{builtin("DIRECT"), policy("节点选择")},
			},
			{
				Name: "全球直连", Icon: "🎯", Type: "select",
				Members: []Member{builtin("DIRECT"), policy("节点选择")},
			},
			{
				Name: "广告拦截", Icon: "🛑", Type: "select",
				Members: []Member{builtin("REJECT"), builtin("DIRECT")},
			},
			{
				Name: "漏网之鱼", Icon: "🐟", Type: "select",
				Members: []Member{policy("节点选择"), builtin("DIRECT"), policy("自动选择")},
			},
		},
		Rules: []Rule{
			rule("GEOSITE", "category-ads-all", "广告拦截"),
			rule("GEOSITE", "private", "全球直连"),
			rule("GEOSITE", "openai", "AI 服务"),
			rule("GEOSITE", "anthropic", "AI 服务"),
			rule("GEOSITE", "google-gemini", "AI 服务"),
			rule("GEOSITE", "telegram", "电报消息"),
			rule("GEOSITE", "youtube", "国外媒体"),
			rule("GEOSITE", "netflix", "国外媒体"),
			rule("GEOSITE", "disney", "国外媒体"),
			rule("GEOSITE", "spotify", "国外媒体"),
			rule("GEOSITE", "bilibili", "全球直连"),
			rule("GEOSITE", "apple-cn", "苹果服务"),
			rule("GEOSITE", "microsoft@cn", "微软服务"),
			rule("GEOSITE", "geolocation-!cn", "节点选择"),
			rule("GEOSITE", "cn", "全球直连"),
			ipRule("GEOIP", "telegram", "电报消息", true),
			ipRule("GEOIP", "private", "全球直连", true),
			ipRule("GEOIP", "cn", "全球直连", false),
		},
		Final: policy("漏网之鱼"),
	}
}
