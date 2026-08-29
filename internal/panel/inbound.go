package panel

import (
	"encoding/json"
	"strings"
)

// InboundMembership maps client email to the inbound ids that client is
// attached to on this panel, read from each inbound's settings.clients[].
// Emails are compared case-insensitively because 3x-ui stores them as typed
// but matches them loosely.
func InboundMembership(inbounds []Inbound) map[string][]int {
	out := map[string][]int{}
	for _, ib := range inbounds {
		for _, email := range settingsClientEmails(ib.Settings) {
			key := strings.ToLower(email)
			out[key] = append(out[key], ib.ID)
		}
	}
	return out
}

func settingsClientEmails(settings string) []string {
	if strings.TrimSpace(settings) == "" {
		return nil
	}
	var parsed struct {
		Clients []struct {
			Email string `json:"email"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return nil
	}
	out := make([]string, 0, len(parsed.Clients))
	for _, c := range parsed.Clients {
		if c.Email != "" {
			out = append(out, c.Email)
		}
	}
	return out
}

// TrafficByEmail flattens per-inbound client stats into one row per email;
// 3x-ui keys client traffic by a panel-unique email, so there is at most one.
func TrafficByEmail(inbounds []Inbound) map[string]ClientTraffic {
	out := map[string]ClientTraffic{}
	for _, ib := range inbounds {
		for _, st := range ib.ClientStats {
			if st.Email == "" {
				continue
			}
			out[strings.ToLower(st.Email)] = st
		}
	}
	return out
}
