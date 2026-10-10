package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWriteInboundPreservesTargetClientsAndDoesNotCopySourceIdentity(t *testing.T) {
	raw := `{"id":7,"userId":1,"up":123,"down":456,"nodeId":9,"originNodeGuid":"target-node","lastTrafficResetTime":999,"port":443,"protocol":"vless","listen":"","enable":true,"settings":{"clients":[{"id":"target-user","email":"alice","flow":"xtls-rprx-vision","totalGB":12345,"expiryTime":9999,"unknownCredential":"keep"}],"decryption":"none"},"streamSettings":{"network":"tcp","security":"reality"},"sniffing":{},"clientStats":[{"up":88}]}`
	var target Inbound
	if err := json.Unmarshal([]byte(raw), &target); err != nil {
		t.Fatal(err)
	}
	source := `{"id":99,"nodeId":123,"originNodeGuid":"source-node","up":900,"down":900,"port":9443,"protocol":"vless","listen":"127.0.0.1","enable":true,"settings":{"clients":[{"id":"source-user"}],"decryption":"server-auth"},"streamSettings":{"network":"tcp","security":"reality","unknownExtension":{"value":42}},"sniffing":{}}`
	config, err := EditableConfiguration(source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/inbounds/update/7" {
			t.Errorf("path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["id"] != float64(7) || body["nodeId"] != float64(9) || body["originNodeGuid"] != "target-node" || body["up"] != float64(123) || body["down"] != float64(456) || body["lastTrafficResetTime"] != float64(999) {
			t.Errorf("target fields overwritten: %v", body)
		}
		var targetSettings map[string]any
		json.Unmarshal([]byte(target.Settings), &targetSettings)
		got := body["settings"].(map[string]any)
		if !reflect.DeepEqual(got["clients"], targetSettings["clients"]) || got["decryption"] != "server-auth" {
			t.Errorf("incorrect settings: %v", got)
		}
		if body["streamSettings"].(map[string]any)["unknownExtension"] == nil {
			t.Error("extension discarded")
		}
		if _, ok := body["clientStats"]; ok {
			t.Error("traffic records copied")
		}
		w.Write([]byte(`{"success":true,"obj":{"id":7}}`))
	}))
	defer server.Close()
	if _, err := New(server.URL, "tok", false).WriteInbound(context.Background(), config, &target); err != nil {
		t.Fatal(err)
	}
}

func TestWriteNewInboundDoesNotGrantSourceClients(t *testing.T) {
	config, err := EditableConfiguration(`{"id":7,"nodeId":1,"port":443,"protocol":"vless","listen":"","enable":true,"settings":{"clients":[{"id":"source-user"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/inbounds/add" {
			t.Errorf("path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["id"]; ok {
			t.Error("source ID copied")
		}
		if _, ok := body["nodeId"]; ok {
			t.Error("source node copied")
		}
		if clients := body["settings"].(map[string]any)["clients"].([]any); len(clients) != 0 {
			t.Error("source clients copied")
		}
		w.Write([]byte(`{"success":true,"obj":{"id":8}}`))
	}))
	defer server.Close()
	result, err := New(server.URL, "tok", false).WriteInbound(context.Background(), config, nil)
	if err != nil || result.ID != 8 {
		t.Fatalf("result %v %v", result, err)
	}
}
