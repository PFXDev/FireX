package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigurationPreservesOptionsWithoutClientAccounts(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		settings := `{"decryption":"none","clients":[{"email":"alice","id":"user-secret","flow":"xtls-rprx-vision"}]}`
		stream := `{"network":"raw","security":"reality","realitySettings":{"privateKey":"server-key","shortIds":["ab"]},"futureOption":{"value":42}}`
		wire := map[string]any{"id": 7, "protocol": "vless", "settings": json.RawMessage(settings), "streamSettings": json.RawMessage(stream), "sniffing": json.RawMessage(`{"enabled":true}`), "clientStats": []any{map[string]any{"email": "alice"}}, "futureTopLevel": true}
		if legacy {
			wire["settings"], wire["streamSettings"], wire["sniffing"] = settings, stream, `{"enabled":true}`
		}
		data, _ := json.Marshal(wire)
		var inbound Inbound
		if err := json.Unmarshal(data, &inbound); err != nil {
			t.Fatal(err)
		}
		config := inbound.Configuration()
		if strings.Contains(config, "alice") || strings.Contains(config, "user-secret") || strings.Contains(config, "clientStats") {
			t.Fatalf("client data in configuration: %s", config)
		}
		for _, want := range []string{`"futureTopLevel":true`, `"futureOption":{"value":42}`, `"privateKey":"server-key"`, `"settings":{"decryption":"none"}`} {
			if !strings.Contains(config, want) {
				t.Errorf("missing %s in %s", want, config)
			}
		}
		if inbound.ClientFlows()["alice"] != VisionFlow {
			t.Error("configuration extraction changed the live clients")
		}
	}
}

func TestVisionCapability(t *testing.T) {
	for _, tc := range []struct {
		protocol, network, security string
		disabled, supported         bool
	}{
		{"vless", "tcp", "reality", false, true},
		{"vless", "raw", "tls", false, true},
		{"vless", "tcp", "reality", true, false},
		{"vless", "ws", "tls", false, false},
		{"vless", "grpc", "reality", false, false},
		{"vless", "xhttp", "reality", false, false},
		{"vless", "tcp", "none", false, false},
		{"vmess", "tcp", "tls", false, false},
		{"vless", "", "", false, false},
	} {
		if got := VisionReason(tc.protocol, tc.network, tc.security, tc.disabled) == ""; got != tc.supported {
			t.Errorf("%+v: supported = %v", tc, got)
		}
	}
}

func TestUpdateClientInboundsSendsFilterAndExplicitEmptyFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/panel/api/clients/update/alice@FireX" || r.URL.Query().Get("inboundIds") != "2,7" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if flow, exists := body["flow"]; !exists || flow != "" {
			t.Errorf("flow = %v, exists = %v", flow, exists)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	client := New(server.URL, "tok", false)
	if err := client.UpdateClientInbounds(context.Background(), "alice@FireX", RemoteClient{Email: "alice@FireX"}, []int{2, 7}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateClientInbounds(context.Background(), "alice@FireX", RemoteClient{}, nil); err == nil {
		t.Fatal("an empty inbound filter must not become a panel-wide update")
	}
}
