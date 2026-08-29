// Package model holds FireX's persisted schema.
//
// Ownership split: a Panel is a remote 3x-ui install, a Node is one inbound on
// that panel rendered as one client-visible proxy, and provisioning state is
// tracked per (User, Panel) because 3x-ui keys client traffic by email panel-wide.
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

// Node is one inbound on one panel, surfaced to clients as a single proxy.
// Rows are discovered from the panel; the display fields below are FireX-owned
// and survive rediscovery.
type Node struct {
	ID      uint `json:"id" gorm:"primaryKey;autoIncrement"`
	PanelID uint `json:"panelId" gorm:"index:idx_node_panel_inbound,unique,priority:1;not null"`
	// InboundID is the inbound's id on the remote panel, not a FireX id.
	InboundID int `json:"inboundId" gorm:"index:idx_node_panel_inbound,unique,priority:2;not null"`

	InboundTag    string `json:"inboundTag"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	RemoteRemark  string `json:"remoteRemark"`
	RemoteEnabled bool   `json:"remoteEnabled"`

	Name       string  `json:"name"`
	Region     string  `json:"region" gorm:"index"`
	Emoji      string  `json:"emoji"`
	Tags       string  `json:"tags"`
	SortOrder  int     `json:"sortOrder" gorm:"default:100"`
	Enabled    bool    `json:"enabled" gorm:"default:false"`
	UDP        bool    `json:"udp" gorm:"default:true"`
	Multiplier float64 `json:"multiplier" gorm:"default:1"`

	// Missing marks a node whose inbound vanished upstream; kept so plan
	// membership and history survive a transient panel outage.
	Missing    bool  `json:"missing"`
	LastSeenAt int64 `json:"lastSeenAt"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// DisplayName is the remark clients see for this node.
func (n *Node) DisplayName() string {
	name := n.Name
	if name == "" {
		name = n.RemoteRemark
	}
	if name == "" {
		name = n.InboundTag
	}
	if n.Emoji != "" {
		return n.Emoji + " " + name
	}
	return name
}

type Plan struct {
	ID           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name         string `json:"name" gorm:"uniqueIndex;not null"`
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

type PlanNode struct {
	PlanID uint `json:"planId" gorm:"primaryKey"`
	NodeID uint `json:"nodeId" gorm:"primaryKey;index"`
}

type User struct {
	ID       uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Username string `json:"username" gorm:"uniqueIndex;not null"`
	// UUID doubles as the VLESS/VMess id and the Trojan/Shadowsocks password so
	// one identity works across every protocol a node might use.
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
// finest granularity the remote API exposes — not per-node.
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
		&Admin{}, &Session{}, &Panel{}, &Node{}, &Plan{},
		&PlanNode{}, &User{}, &UserPanel{}, &Setting{},
	}
}
