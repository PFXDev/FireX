package panel

import (
	"encoding/json"
	"testing"
)

func TestInboundUnmarshalJSONEmptySettings(t *testing.T) {
	var inbound Inbound
	if err := json.Unmarshal([]byte(`{"id":1,"settings":null}`), &inbound); err != nil {
		t.Fatalf("unmarshal inbound: %v", err)
	}
	if inbound.Settings != "" || inbound.StreamSettings != "" || inbound.Sniffing != "" {
		t.Fatalf("settings = %q, streamSettings = %q, sniffing = %q; want empty", inbound.Settings, inbound.StreamSettings, inbound.Sniffing)
	}
}
