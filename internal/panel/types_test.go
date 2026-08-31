package panel

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestInboundUnmarshalJSONAcceptsSettingsShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "modern nested objects",
			body: `{
				"id": 7,
				"settings": {"clients":[{"email":"alice@example.com"}],"decryption":"none"},
				"streamSettings": {"network":"tcp","security":"none"},
				"sniffing": {"enabled":true}
			}`,
		},
		{
			name: "legacy encoded strings",
			body: `{
				"id": 7,
				"settings": "{\"clients\":[{\"email\":\"alice@example.com\"}],\"decryption\":\"none\"}",
				"streamSettings": "{\"network\":\"tcp\",\"security\":\"none\"}",
				"sniffing": "{\"enabled\":true}"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inbound Inbound
			if err := json.Unmarshal([]byte(tt.body), &inbound); err != nil {
				t.Fatalf("unmarshal inbound: %v", err)
			}

			var settings struct {
				Clients []struct {
					Email string `json:"email"`
				} `json:"clients"`
				Decryption string `json:"decryption"`
			}
			if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
				t.Fatalf("settings were not normalized to JSON text: %v", err)
			}
			if len(settings.Clients) != 1 || settings.Clients[0].Email != "alice@example.com" {
				t.Fatalf("settings clients = %#v", settings.Clients)
			}
			if settings.Decryption != "none" {
				t.Fatalf("settings decryption = %q, want none", settings.Decryption)
			}

			var stream map[string]any
			if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
				t.Fatalf("streamSettings were not normalized to JSON text: %v", err)
			}
			if stream["network"] != "tcp" || stream["security"] != "none" {
				t.Fatalf("streamSettings = %#v", stream)
			}

			var sniffing struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal([]byte(inbound.Sniffing), &sniffing); err != nil {
				t.Fatalf("sniffing was not normalized to JSON text: %v", err)
			}
			if !sniffing.Enabled {
				t.Fatal("sniffing.enabled = false, want true")
			}

			membership := InboundMembership([]Inbound{inbound})
			if got := membership["alice@example.com"]; !reflect.DeepEqual(got, []int{7}) {
				t.Fatalf("membership = %#v, want [7]", got)
			}
		})
	}
}

func TestInboundUnmarshalJSONEmptySettings(t *testing.T) {
	var inbound Inbound
	if err := json.Unmarshal([]byte(`{"id":1,"settings":null}`), &inbound); err != nil {
		t.Fatalf("unmarshal inbound: %v", err)
	}
	if inbound.Settings != "" || inbound.StreamSettings != "" || inbound.Sniffing != "" {
		t.Fatalf("settings = %q, streamSettings = %q, sniffing = %q; want empty", inbound.Settings, inbound.StreamSettings, inbound.Sniffing)
	}
}
