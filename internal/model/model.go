// Package model holds FireX's persisted schema.
//
// Ownership split: a Panel is a remote 3x-ui install, an Inbound is one inbound
// on that panel rendered as one client-visible proxy, and provisioning state is
// tracked per (User, Panel) because 3x-ui keys client traffic by email panel-wide.
//
// Routing is two axes meeting in an Egress: a Policy is a reusable rule list
// every user shares, a Profile is what one tier of user may reach, and the
// Egress at their intersection says which node groups that policy uses for that
// profile. A Plan carries quota and binds one Profile; nothing else decides
// which inbounds a user is provisioned onto.
package model

type Admin struct {
	ID           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash string `json:"-" gorm:"not null"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type Session struct {
	Token     string `json:"-" gorm:"primaryKey"`
	AdminID   uint   `json:"adminId" gorm:"index"`
	ExpiresAt int64  `json:"expiresAt" gorm:"index"`
	CreatedAt int64  `json:"createdAt"`
}

// PanelStatus values for Panel.Status.
const (
	PanelStatusUnknown = "unknown"
	PanelStatusOnline  = "online"
	PanelStatusOffline = "offline"
)

type Panel struct {
	ID            uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name          string `json:"name" gorm:"uniqueIndex;not null"`
	BaseURL       string `json:"baseUrl" gorm:"not null"`
	APIToken      string `json:"apiToken" gorm:"not null"`
	SkipTLSVerify bool   `json:"skipTlsVerify"`
	Enabled       bool   `json:"enabled" gorm:"default:true"`
	Remark        string `json:"remark"`

	Status      string `json:"status" gorm:"default:unknown"`
	LastError   string `json:"lastError"`
	LastSeenAt  int64  `json:"lastSeenAt"`
	XrayVersion string `json:"xrayVersion"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// Inbound is one inbound on one panel, surfaced to clients as a single proxy.
// Rows are discovered from the panel; the display fields below are FireX-owned
// and survive rediscovery. Classification (region, line) lives on the node
// group instead — an inbound is only ever managed through the groups it is in.
type Inbound struct {
	ID      uint `json:"id" gorm:"primaryKey;autoIncrement"`
	PanelID uint `json:"panelId" gorm:"index:idx_inbound_panel_remote,unique,priority:1;not null"`
	// RemoteID is the inbound's id on the remote panel, not a FireX id.
	RemoteID int `json:"remoteId" gorm:"index:idx_inbound_panel_remote,unique,priority:2;not null"`

	InboundTag    string `json:"inboundTag"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	RemoteRemark  string `json:"remoteRemark"`
	RemoteEnabled bool   `json:"remoteEnabled"`

	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	SortOrder int    `json:"sortOrder" gorm:"default:100"`
	Enabled   bool   `json:"enabled" gorm:"default:false"`
	UDP       bool   `json:"udp" gorm:"default:true"`

	// Missing marks an inbound that vanished upstream; kept so group membership
	// and the admin's labels survive a transient panel outage.
	Missing    bool  `json:"missing"`
	LastSeenAt int64 `json:"lastSeenAt"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// DisplayName is the proxy name clients see for this inbound.
func (i *Inbound) DisplayName() string {
	name := i.Name
	if name == "" {
		name = i.RemoteRemark
	}
	if name == "" {
		name = i.InboundTag
	}
	if i.Emoji != "" {
		return i.Emoji + " " + name
	}
	return name
}

// Proxy-group types, mirroring the mihomo types FireX generates.
const (
	GroupTypeURLTest     = "url-test"
	GroupTypeSelect      = "select"
	GroupTypeFallback    = "fallback"
	GroupTypeLoadBalance = "load-balance"
)

// NodeGroup bundles hand-picked inbounds into one client-visible proxy-group,
// normally one region on one line ("🇭🇰 香港 IEPL"). It is the finest unit the
// rest of FireX addresses: profiles whitelist groups, never single inbounds.
// Membership is explicit rather than derived from any text field, so retyping a
// label cannot silently reshape a user's subscription.
type NodeGroup struct {
	ID    uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name  string `json:"name" gorm:"uniqueIndex;not null"`
	Emoji string `json:"emoji"`

	Type      string `json:"type" gorm:"default:url-test"`
	TestURL   string `json:"testUrl"`
	Interval  int    `json:"interval" gorm:"default:300"`
	Tolerance int    `json:"tolerance" gorm:"default:50"`

	// Multiplier is the line's traffic multiplier. Display only: FireX counts
	// raw bytes and does not bill.
	Multiplier float64 `json:"multiplier" gorm:"default:1"`

	SortOrder int    `json:"sortOrder" gorm:"default:100"`
	Enabled   bool   `json:"enabled" gorm:"default:true"`
	Remark    string `json:"remark"`

	// GORM rewrites a field named UpdatedAt on every save, and defaults to
	// unix seconds for an integer one; the tags keep both stamps in the
	// milliseconds the rest of FireX speaks.
	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// DisplayName is the proxy-group name clients see.
func (g *NodeGroup) DisplayName() string {
	if g.Emoji != "" {
		return g.Emoji + " " + g.Name
	}
	return g.Name
}

// Well-known NodeGroupTag keys. The set is open — an operator may add their own
// axis — but these are the ones the UI offers first.
const (
	TagKeyRegion  = "地区"
	TagKeyLine    = "线路"
	TagKeyLanding = "落地"
)

// NodeGroupTag is one key/value label on a node group, e.g. 线路=CN2GIA. Tags
// drive nothing at render time; they exist so a long group list stays sortable
// and filterable. One value per key per group.
type NodeGroupTag struct {
	GroupID uint   `json:"-" gorm:"primaryKey"`
	Key     string `json:"key" gorm:"primaryKey"`
	Value   string `json:"value"`
}

// NodeGroupInbound is one inbound's membership in one group. An inbound may
// belong to several groups — a Hong Kong relay can sit in both "🇭🇰 香港" and
// "中转".
type NodeGroupInbound struct {
	GroupID   uint `json:"groupId" gorm:"primaryKey"`
	InboundID uint `json:"inboundId" gorm:"primaryKey;index"`
}

// Policy is one reusable rule list plus the identity it takes in the client's
// group list ("🤖 AI 服务"). Policies are global: every profile sees the same
// list in the same order. What differs per profile is the Egress.
type Policy struct {
	ID   uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name string `json:"name" gorm:"uniqueIndex;not null"`
	Icon string `json:"icon"`

	// IsFinal marks the policy that renders as MATCH. It always sorts last and
	// carries no rules of its own.
	IsFinal   bool   `json:"isFinal"`
	SortOrder int    `json:"sortOrder" gorm:"default:100"`
	Enabled   bool   `json:"enabled" gorm:"default:true"`
	Remark    string `json:"remark"`

	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// DisplayName is the proxy-group name clients see for this policy.
func (p *Policy) DisplayName() string {
	if p.Icon != "" {
		return p.Icon + " " + p.Name
	}
	return p.Name
}

// Rule is one line of a policy's list. Rule order within a policy is SortOrder;
// order across policies is Policy.SortOrder, so precedence is a property of the
// rule content and never varies by profile.
type Rule struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	PolicyID  uint   `json:"policyId" gorm:"index;not null"`
	SortOrder int    `json:"sortOrder"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	NoResolve bool   `json:"noResolve"`
	Disabled  bool   `json:"disabled"`
}

// Profile is one tier's routing: the node groups its users may reach, plus the
// column of Egress overrides that differ from the defaults. Several plans may
// share one profile — monthly and yearly VIP differ in price, not in routing.
type Profile struct {
	ID   uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name string `json:"name" gorm:"uniqueIndex;not null"`
	// AllGroups whitelists every enabled node group instead of an explicit set,
	// so a single-tier install never has to remember to add each new group.
	AllGroups bool   `json:"allGroups"`
	SortOrder int    `json:"sortOrder" gorm:"default:100"`
	Enabled   bool   `json:"enabled" gorm:"default:true"`
	Remark    string `json:"remark"`

	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// ProfileNodeGroup whitelists one node group for one profile. This is the only
// thing that decides which inbounds a user is provisioned onto — editing rules
// or egresses never reaches a panel.
type ProfileNodeGroup struct {
	ProfileID uint `json:"profileId" gorm:"primaryKey"`
	GroupID   uint `json:"groupId" gorm:"primaryKey;index"`
}

// DefaultProfileID is the Egress.ProfileID of the default column, the one every
// profile falls back to when it has no override of its own.
const DefaultProfileID = 0

// Egress is one cell of the routing matrix: how policy P behaves for profile F.
// A profile with no row here inherits the default column; a row with Hidden set
// drops the policy for that profile entirely.
type Egress struct {
	ID       uint `json:"id" gorm:"primaryKey;autoIncrement"`
	PolicyID uint `json:"policyId" gorm:"index:idx_egress_cell,unique,priority:1;not null"`
	// ProfileID is DefaultProfileID for the default column.
	ProfileID uint `json:"profileId" gorm:"index:idx_egress_cell,unique,priority:2"`

	Type      string `json:"type" gorm:"default:select"`
	TestURL   string `json:"testUrl"`
	Interval  int    `json:"interval" gorm:"default:300"`
	Tolerance int    `json:"tolerance" gorm:"default:50"`
	// Hidden emits neither the proxy-group nor the policy's rules; that traffic
	// falls through to whatever matches next.
	Hidden bool `json:"hidden"`
}

// EgressMember kinds. Ref carries a bare Name so an emoji edit can never orphan
// a reference.
const (
	// MemberNodeGroup references one node group by name.
	MemberNodeGroup = "node-group"
	// MemberPolicy references another policy by name.
	MemberPolicy = "policy"
	// MemberBuiltin is a mihomo policy such as DIRECT; Ref carries it verbatim.
	MemberBuiltin = "builtin"
	// MemberAllNodeGroups expands to every node group the profile whitelists,
	// in node-group order. This is how one default column serves every tier.
	MemberAllNodeGroups = "all-node-groups"
	// MemberAllInbounds expands to every inbound the profile grants, in display
	// order — a flat list for users who want to pick one proxy by hand.
	MemberAllInbounds = "all-inbounds"
)

// BuiltinPolicies are the non-group members mihomo understands.
var BuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}

type EgressMember struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	EgressID  uint   `json:"egressId" gorm:"index;not null"`
	SortOrder int    `json:"sortOrder"`
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
}

// Plan is the commercial side only: quota, duration, device cap, and which
// routing profile its users get.
type Plan struct {
	ID           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name         string `json:"name" gorm:"uniqueIndex;not null"`
	ProfileID    uint   `json:"profileId" gorm:"index"`
	TrafficBytes int64  `json:"trafficBytes"` // 0 = unlimited
	DurationDays int    `json:"durationDays"` // 0 = no expiry
	DeviceLimit  int    `json:"deviceLimit"`  // maps to 3x-ui limitIp; 0 = unlimited
	SpeedNote    string `json:"speedNote"`
	Enabled      bool   `json:"enabled" gorm:"default:true"`
	SortOrder    int    `json:"sortOrder" gorm:"default:100"`
	Remark       string `json:"remark"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

type User struct {
	ID       uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Username string `json:"username" gorm:"uniqueIndex;not null"`
	// UUID doubles as the VLESS/VMess id and the Trojan/Shadowsocks password so
	// one identity works across every protocol an inbound might use.
	UUID     string `json:"uuid" gorm:"not null"`
	SubToken string `json:"subToken" gorm:"uniqueIndex;not null"`

	PlanID  uint `json:"planId" gorm:"index"`
	Enabled bool `json:"enabled" gorm:"default:true"`

	ExpiresAt    int64 `json:"expiresAt"`    // ms epoch, 0 = never
	TrafficLimit int64 `json:"trafficLimit"` // bytes, 0 = unlimited
	Upload       int64 `json:"upload"`
	Download     int64 `json:"download"`

	// Depleted is set by the traffic collector when the aggregate crosses the
	// limit, so the reason a user went dark survives a limit bump.
	Depleted  bool   `json:"depleted"`
	Remark    string `json:"remark"`
	LastSubAt int64  `json:"lastSubAt"`
	LastSubUA string `json:"lastSubUa"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// Active reports whether the user should currently have working proxies.
func (u *User) Active(nowMs int64) bool {
	if !u.Enabled || u.Depleted {
		return false
	}
	if u.ExpiresAt > 0 && u.ExpiresAt <= nowMs {
		return false
	}
	if u.TrafficLimit > 0 && u.Upload+u.Download >= u.TrafficLimit {
		return false
	}
	return true
}

// UserPanel sync states.
const (
	SyncStatePending = "pending"
	SyncStateSynced  = "synced"
	SyncStateFailed  = "failed"
)

// UserPanel is the provisioning record for one user on one panel. 3x-ui keys a
// client (and its traffic counters) by a panel-unique email, so this is the
// finest granularity the remote API exposes — not per-inbound.
type UserPanel struct {
	UserID  uint   `json:"userId" gorm:"primaryKey"`
	PanelID uint   `json:"panelId" gorm:"primaryKey;index"`
	Email   string `json:"email" gorm:"index"`

	// InboundIDs is the remote inbound id set currently provisioned, csv.
	InboundIDs string `json:"inboundIds"`
	// DesiredHash fingerprints the client fields last pushed, so a reconcile
	// pass that changes nothing skips the write entirely.
	DesiredHash string `json:"-"`
	State       string `json:"state" gorm:"default:pending"`
	LastError   string `json:"lastError"`

	// LastUp/LastDown are the last raw counters read from the panel. Deltas
	// against them accumulate into User.Upload/Download; a counter that moved
	// backwards means the panel reset it, so the new value is the whole delta.
	LastUp   int64 `json:"lastUp"`
	LastDown int64 `json:"lastDown"`

	UpdatedAt int64 `json:"updatedAt"`
}

// Setting is the key/value store for admin-editable runtime config such as the
// Clash template.
type Setting struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

// AllModels is the AutoMigrate list.
func AllModels() []any {
	return []any{
		&Admin{}, &Session{}, &Panel{}, &Inbound{},
		&NodeGroup{}, &NodeGroupTag{}, &NodeGroupInbound{},
		&Policy{}, &Rule{},
		&Profile{}, &ProfileNodeGroup{}, &Egress{}, &EgressMember{},
		&Plan{}, &User{}, &UserPanel{}, &Setting{},
	}
}
