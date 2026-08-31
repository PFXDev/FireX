package panel

import (
	"bytes"
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
	// 3x-ui v3.7+ returns these as nested JSON objects, while older panels
	// return JSON-encoded strings. UnmarshalJSON normalizes both shapes to the
	// JSON text consumed by the membership parser.
	Settings       string `json:"settings"`
	StreamSettings string `json:"streamSettings"`
	Sniffing       string `json:"sniffing"`
}

// UnmarshalJSON accepts both generations of the 3x-ui inbound wire format.
// The panel stores these fields as strings internally, but since v3.7 its API
// marshals valid JSON text as nested JSON instead of an escaped string.
func (i *Inbound) UnmarshalJSON(data []byte) error {
	type alias Inbound
	aux := struct {
		*alias
		Settings       json.RawMessage `json:"settings"`
		StreamSettings json.RawMessage `json:"streamSettings"`
		Sniffing       json.RawMessage `json:"sniffing"`
	}{
		alias: (*alias)(i),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	i.Settings = jsonTextFromRaw(aux.Settings)
	i.StreamSettings = jsonTextFromRaw(aux.StreamSettings)
	i.Sniffing = jsonTextFromRaw(aux.Sniffing)
	return nil
}

// jsonTextFromRaw mirrors 3x-ui's own compatibility decoder: null or a
// missing field becomes empty, a legacy JSON string is unwrapped, and modern
// nested JSON is preserved as text.
func jsonTextFromRaw(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return text
		}
	}
	return string(trimmed)
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
