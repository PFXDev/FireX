package panel

import (
	"encoding/json"
	"errors"
)

// ErrClientMissing means the panel has no client with that email.
var ErrClientMissing = errors.New("panel: client not found")

type Status struct {
	Cpu float64 `json:"cpu"`
	Mem struct {
		Current uint64 `json:"current"`
		Total   uint64 `json:"total"`
	} `json:"mem"`
	Disk struct {
		Current uint64 `json:"current"`
		Total   uint64 `json:"total"`
	} `json:"disk"`
	Xray struct {
		State    string `json:"state"`
		ErrorMsg string `json:"errorMsg"`
		Version  string `json:"version"`
	} `json:"xray"`
	PanelVersion string `json:"panelVersion"`
	Uptime       uint64 `json:"uptime"`
}

// ClientTraffic mirrors the panel's per-client counters. Total is bytes despite
// the sibling field on RemoteClient being named totalGB.
type ClientTraffic struct {
	InboundID  int    `json:"inboundId"`
	Enable     bool   `json:"enable"`
	Email      string `json:"email"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	ExpiryTime int64  `json:"expiryTime"`
	Total      int64  `json:"total"`
	LastOnline int64  `json:"lastOnline"`
}

type Inbound struct {
	ID          int             `json:"id"`
	Up          int64           `json:"up"`
	Down        int64           `json:"down"`
	Total       int64           `json:"total"`
	Remark      string          `json:"remark"`
	Enable      bool            `json:"enable"`
	Listen      string          `json:"listen"`
	Port        int             `json:"port"`
	Protocol    string          `json:"protocol"`
	Tag         string          `json:"tag"`
	ClientStats []ClientTraffic `json:"clientStats"`
	// Settings and StreamSettings arrive as JSON-encoded strings.
	Settings       string `json:"settings"`
	StreamSettings string `json:"streamSettings"`
}

// RemoteClient is the subset of 3x-ui's client model FireX writes. Fields the
// panel owns (traffic counters, reset bookkeeping) are deliberately absent.
//
// TotalGB is bytes — the name is 3x-ui's, not a unit.
type RemoteClient struct {
	ID         string `json:"id,omitempty"`
	Password   string `json:"password,omitempty"`
	Flow       string `json:"flow,omitempty"`
	Email      string `json:"email"`
	LimitIP    int    `json:"limitIp"`
	TotalGB    int64  `json:"totalGB"`
	ExpiryTime int64  `json:"expiryTime"`
	Enable     bool   `json:"enable"`
	SubID      string `json:"subId,omitempty"`
	Group      string `json:"group,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

type ClientDetail struct {
	Client      RemoteClient    `json:"client"`
	InboundIDs  []int           `json:"inboundIds"`
	UsedTraffic int64           `json:"usedTraffic"`
	Raw         json.RawMessage `json:"-"`
}
