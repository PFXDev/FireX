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

import "time"

// Admin roles. A super admin can do everything, including managing other
// admins; an operator manages users and may only read the overview and plans,
// plus the configuration pages when Admin.CanViewConfig is set.
const (
	AdminRoleSuperAdmin = "super_admin"
	AdminRoleOperator   = "operator"
)

type Admin struct {
	ID           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash string `json:"-" gorm:"not null"`
	// Role is AdminRoleSuperAdmin or AdminRoleOperator. Rows from before roles
	// existed migrate in blank and are promoted, since they had full access.
	Role string `json:"role" gorm:"not null;default:''"`
	// CanViewConfig lets an operator read panels, inbounds, node groups,
	// routing and the subscription template without changing any of them.
	// Super admins can always read and write, so it is ignored for them.
	CanViewConfig bool  `json:"canViewConfig" gorm:"not null;default:false"`
	CreatedAt     int64 `json:"createdAt"`
	UpdatedAt     int64 `json:"updatedAt"`
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

	// TrafficLimit caps what all FireX users together may move through this
	// panel in one period, in bytes; 0 is unlimited. Over it, every client
	// FireX keeps here is disabled and the panel's inbounds leave every
	// subscription until the period starts over. Clients created on the
	// panel by hand are not counted.
	TrafficLimit int64 `json:"trafficLimit" gorm:"not null;default:0"`
	// TrafficResetDay is the day of the month the period starts over at
	// midnight, clamped to short months; 0 never resets on its own.
	TrafficResetDay int `json:"trafficResetDay" gorm:"not null;default:0"`
	// Upload/Download are FireX clients' usage on this panel in the current
	// period, folded in by the traffic collector. NOT NULL so that panels
	// predating the columns count from zero: NULL + n stays NULL.
	Upload   int64 `json:"upload" gorm:"not null;default:0"`
	Download int64 `json:"download" gorm:"not null;default:0"`
	// TrafficPeriodStart is when the current period began; 0 means CreatedAt.
	TrafficPeriodStart int64 `json:"trafficPeriodStart" gorm:"not null;default:0"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// OverQuota reports whether the panel used up its period's traffic.
func (p *Panel) OverQuota() bool {
	return p.TrafficLimit > 0 && p.Upload+p.Download >= p.TrafficLimit
}

// NextTrafficReset is when the panel's period ends, as ms epoch at midnight in
// loc, or 0 when it never resets on its own. Like a user's, it is the first
// boundary after the period start, so one already passed means a reset is due.
func (p *Panel) NextTrafficReset(loc *time.Location) int64 {
	if p.TrafficResetDay <= 0 {
		return 0
	}
	start := p.TrafficPeriodStart
	if start == 0 {
		start = p.CreatedAt
	}
	since := time.UnixMilli(start).In(loc)
	next := monthDay(since.Year(), since.Month(), p.TrafficResetDay, loc)
	if !next.After(since) {
		next = monthDay(since.Year(), since.Month()+1, p.TrafficResetDay, loc)
	}
	return next.UnixMilli()
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
	Listen        string `json:"listen"`
	Network       string `json:"network"`
	Security      string `json:"security"`
	DisableFlow   bool   `json:"disableFlow"`
	// Config is the last discovered inbound configuration, without client
	// accounts or traffic counters. Only the admin detail endpoint exposes it.
	Config string `json:"-"`

	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	SortOrder int    `json:"sortOrder" gorm:"default:100"`
	Enabled   bool   `json:"enabled" gorm:"default:false"`
	UDP       bool   `json:"udp" gorm:"default:true"`
	// Nil preserves each existing client's flow when upgrading from a version
	// without this setting. Discovery explicitly disables Vision for new rows.
	Vision *bool `json:"vision"`
	// XHTTPTuning renders an auto-mode XHTTP inbound for mihomo as stream-up
	// with connection reuse. Other inbounds and share links ignore it.
	XHTTPTuning bool `json:"xhttpTuning" gorm:"default:true"`

	// PublicAddress and PublicPort are the endpoint clients actually dial when
	// it differs from what the panel advertises. A panel only knows where xray
	// listens; an inbound parked on a loopback port behind an SNI router on
	// 443, a relay or a CDN is reached somewhere else entirely. Empty and zero
	// keep the panel's own values.
	PublicAddress string `json:"publicAddress"`
	PublicPort    int    `json:"publicPort"`

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

// RuleSet is a named rule list FireX serves to clients as a mihomo
// rule-provider. A policy uses it through a RULE-SET rule naming it, so a long
// host list stays out of the matrix and survives a preset swap, and clients
// pick up edits on the provider's own refresh rather than the next
// subscription fetch.
type RuleSet struct {
	ID   uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name string `json:"name" gorm:"uniqueIndex;not null"`
	// Payload is classical rule lines without a target, one per line, such as
	// DOMAIN-SUFFIX,hf.co. Blank lines and # comments are kept for the editor
	// and stripped when served.
	Payload string `json:"payload"`
	Remark  string `json:"remark"`

	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
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
	// ClientHidden keeps the group and its rules, but asks compatible clients
	// to hide the group in their UI. It is not an access restriction.
	ClientHidden bool `json:"clientHidden" gorm:"default:false"`
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

// BuiltinDirectV6 is the one builtin FireX supplies itself rather than
// mihomo: a direct outbound that prefers IPv6, rendered into `proxies` only
// when some group references it. Its TCP falls back to IPv4 but its UDP does
// not, so it is an opt-in for clients on good IPv6, never a default.
const BuiltinDirectV6 = "DIRECT-V6"

// BuiltinPolicies are the non-group members a policy may list.
var BuiltinPolicies = []string{"DIRECT", BuiltinDirectV6, "REJECT", "REJECT-DROP", "PASS"}

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
	// TrafficReset is how often a user's usage starts over; see TrafficReset*.
	// Users follow their plan's current value rather than a copy of it.
	TrafficReset string `json:"trafficReset" gorm:"default:none"`
	SpeedNote    string `json:"speedNote"`
	Enabled      bool   `json:"enabled" gorm:"default:true"`
	SortOrder    int    `json:"sortOrder" gorm:"default:100"`
	Remark       string `json:"remark"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// PlanPanelLimit caps what each user on a plan may use on one panel per traffic
// period, on top of the plan's overall quota. Over it, only that user's client
// on that panel goes dark; their other panels keep working.
type PlanPanelLimit struct {
	PlanID       uint  `json:"planId" gorm:"primaryKey"`
	PanelID      uint  `json:"panelId" gorm:"primaryKey;index"`
	TrafficBytes int64 `json:"trafficBytes"`
}

// PlanGroupLimit caps what each user on a plan may use through one node group
// per traffic period. The group's NodeGroupPanelShare splits it into a cap per
// panel, enforced like a PlanPanelLimit; where both apply the tighter wins.
type PlanGroupLimit struct {
	PlanID       uint  `json:"planId" gorm:"primaryKey"`
	GroupID      uint  `json:"groupId" gorm:"primaryKey;index"`
	TrafficBytes int64 `json:"trafficBytes"`
}

// NodeGroupPanelShare is one panel's weight within a node group, in percent:
// the part of a PlanGroupLimit a user may spend on that panel. A group whose
// stored weights do not cover every panel it now spans splits evenly instead.
type NodeGroupPanelShare struct {
	GroupID uint `json:"groupId" gorm:"primaryKey"`
	PanelID uint `json:"panelId" gorm:"primaryKey;index"`
	Weight  int  `json:"weight"`
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
	// TrafficPeriodStart is when the current traffic period began: the last
	// reset, or the moment the user's reset policy last changed. 0 means
	// CreatedAt.
	TrafficPeriodStart int64 `json:"trafficPeriodStart"`

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

// Traffic reset policies for Plan.TrafficReset.
const (
	// TrafficResetNone makes the quota last for the whole plan duration.
	TrafficResetNone = "none"
	// TrafficResetMonthly starts over at midnight on the day of the month the
	// user was created, clamped to the month's last day (the 31st becomes the
	// 30th or the 28th).
	TrafficResetMonthly = "monthly"
	// TrafficResetCalendar starts over at midnight on the 1st of every month.
	TrafficResetCalendar = "calendar"
)

// ValidTrafficReset reports whether mode is a known reset policy.
func ValidTrafficReset(mode string) bool {
	switch mode {
	case TrafficResetNone, TrafficResetMonthly, TrafficResetCalendar:
		return true
	}
	return false
}

// NextTrafficReset is when the user's current traffic period ends under mode,
// as ms epoch at midnight in loc; 0 when the quota never starts over. It is
// the first boundary after the period start, so one already passed means a
// reset is due. Boundaries come from the calendar, not from the period start,
// so an off-schedule reset does not move the next one.
func (u *User) NextTrafficReset(mode string, loc *time.Location) int64 {
	start := u.TrafficPeriodStart
	if start == 0 {
		start = u.CreatedAt
	}
	since := time.UnixMilli(start).In(loc)
	switch mode {
	case TrafficResetCalendar:
		return time.Date(since.Year(), since.Month()+1, 1, 0, 0, 0, 0, loc).UnixMilli()
	case TrafficResetMonthly:
		day := time.UnixMilli(u.CreatedAt).In(loc).Day()
		next := monthDay(since.Year(), since.Month(), day, loc)
		if !next.After(since) {
			next = monthDay(since.Year(), since.Month()+1, day, loc)
		}
		return next.UnixMilli()
	}
	return 0
}

// monthDay is midnight on day of the given month, or on its last day when the
// month is shorter.
func monthDay(year int, month time.Month, day int, loc *time.Location) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	return time.Date(year, month, min(day, last), 0, 0, 0, 0, loc)
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
	// ResetPending marks a traffic reset this panel has not yet confirmed. The
	// baselines stay at the panel's old counters until it does, so usage is not
	// counted twice, and the traffic loop retries the reset.
	ResetPending bool `json:"resetPending"`
	// Upload/Download are the user's usage on this panel in their current
	// traffic period, so they start over with User.Upload/Download. A plan's
	// PlanPanelLimit is checked against them. NOT NULL because reconcile
	// inserts rows without these columns, and NULL + n stays NULL.
	Upload   int64 `json:"upload" gorm:"not null;default:0"`
	Download int64 `json:"download" gorm:"not null;default:0"`

	UpdatedAt int64 `json:"updatedAt"`
}

// Setting is the key/value store for admin-editable runtime config such as the
// Clash template.
type Setting struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

// InboundConfig is the panel configuration FireX owns for one inbound. It
// excludes client accounts, counters and panel identity. Discovery never
// changes it; only an explicit deployment writes it to the panel, so a panel
// whose database was overwritten can be restored from it.
type InboundConfig struct {
	InboundID uint   `json:"inboundId" gorm:"primaryKey;autoIncrement:false"`
	Config    string `json:"-"`
	Revision  uint   `json:"revision" gorm:"not null"`
	// Applied is the panel's readback after the last verified deployment of
	// AppliedRevision. A later discovery that differs from it is drift.
	Applied         string `json:"-"`
	AppliedRevision uint   `json:"appliedRevision"`
	CreatedAt       int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt       int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// InboundTemplate holds parameters shared between nodes, such as a REALITY
// setup. Node-specific listen, port, PROXY and fallback routes are supplied
// when an inbound is created from it; the result is then independent.
type InboundTemplate struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string `json:"name" gorm:"not null"`
	Config    string `json:"-"`
	Revision  uint   `json:"revision" gorm:"not null"`
	CreatedAt int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// InboundDeployment records the panel configuration before a write, even if
// the write fails or its outcome cannot be verified. It never holds clients.
type InboundDeployment struct {
	ID            uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundID     uint   `json:"inboundId" gorm:"index"`
	TemplateID    uint   `json:"templateId"`
	Revision      uint   `json:"revision"`
	PanelID       uint   `json:"panelId"`
	RemoteID      int    `json:"remoteId"`
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	Error         string `json:"error"`
	BeforeConfig  string `json:"-"`
	DesiredConfig string `json:"-"`
	CreatedAt     int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}

// AgentSession states. Open, Submitted and Returned are live: the token works.
const (
	AgentOpen      = "open"
	AgentSubmitted = "submitted"
	AgentReturned  = "returned"
	AgentMerged    = "merged"
	AgentCancelled = "cancelled"
)

// AgentSession hands the configuration to an outside agent for one task. The
// agent reads a snapshot and submits an edited one with a single-use token;
// nothing takes effect until an admin reviews the diff and merges it, which
// ends the session and the token with it. Only the token's hash is stored.
type AgentSession struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	AdminID   uint   `json:"-" gorm:"index"`
	TokenHash string `json:"-" gorm:"uniqueIndex;not null"`
	// Task is what the admin asked for, shown to the agent.
	Task  string `json:"task"`
	State string `json:"state" gorm:"index;not null"`
	// Feedback is the reviewer's note on a returned submission.
	Feedback string `json:"feedback"`

	// Submission is the agent's latest snapshot request, JSON. Summary is its
	// own account of the change; Preview is the dry run computed when it was
	// submitted, against Revision.
	Submission  string `json:"-"`
	Summary     string `json:"summary"`
	Preview     string `json:"-"`
	Revision    string `json:"revision"`
	Submissions int    `json:"submissions"`
	// Result is the merge response, JSON, kept for the finished page.
	Result string `json:"-"`

	ReadAt      int64 `json:"readAt"`
	LastSeenAt  int64 `json:"lastSeenAt"`
	SubmittedAt int64 `json:"submittedAt"`
	ExpiresAt   int64 `json:"expiresAt"`
	FinishedAt  int64 `json:"finishedAt"`
	CreatedAt   int64 `json:"createdAt"`
}

// Live reports whether the session's token still works.
func (a *AgentSession) Live(nowMs int64) bool {
	switch a.State {
	case AgentOpen, AgentSubmitted, AgentReturned:
		return a.ExpiresAt > nowMs
	}
	return false
}

// AuditLog records one configuration change made through the admin API. It
// names who did what to which object, never the request body, so passwords and
// panel credentials stay out of it.
type AuditLog struct {
	ID     uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	// Target is the object's name as it was when the change was made, so a
	// deleted object still reads as something.
	Target    string `json:"target"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"createdAt" gorm:"index"`
}

// AllModels is the AutoMigrate list.
func AllModels() []any {
	return []any{
		&Admin{}, &Session{}, &AgentSession{}, &AuditLog{}, &Panel{}, &Inbound{},
		&NodeGroup{}, &NodeGroupTag{}, &NodeGroupInbound{}, &NodeGroupPanelShare{},
		&Policy{}, &Rule{}, &RuleSet{},
		&Profile{}, &ProfileNodeGroup{}, &Egress{}, &EgressMember{},
		&Plan{}, &PlanPanelLimit{}, &PlanGroupLimit{}, &User{}, &UserPanel{}, &Setting{},
		&InboundConfig{}, &InboundTemplate{}, &InboundDeployment{},
	}
}
