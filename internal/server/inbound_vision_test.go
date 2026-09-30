package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
)

func TestInboundVisionUpdatesPanelAndBothSubscriptionFormats(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	var inbound model.Inbound
	h.db.First(&inbound, "remote_id = ?", 1)
	// Populate the share-link cache before changing the setting.
	h.mustDo(http.MethodGet, "/sub/"+u.SubToken, nil)
	for _, enabled := range []bool{true, false} {
		var result struct {
			SyncError string `json:"syncError"`
		}
		raw := h.mustDo(http.MethodPut, "/api/inbounds/"+itoa(inbound.ID), map[string]any{"name": inbound.Name, "vision": enabled})
		if err := json.Unmarshal(raw, &result); err != nil || result.SyncError != "" {
			t.Fatalf("save: %s (%v)", raw, err)
		}
		want := ""
		if enabled {
			want = panel.VisionFlow
		}
		if got := h.fake.ClientFlow(provision.EmailFor(u), 1); got != want {
			t.Fatalf("remote flow = %q, want %q", got, want)
		}
		if got := h.fake.ClientFlow(provision.EmailFor(u), 2); got != "" {
			t.Fatalf("other inbound flow = %q", got)
		}
		yaml := h.mustDo(http.MethodGet, "/sub/"+u.SubToken+"?target=mihomo", nil)
		if count := strings.Count(string(yaml), "flow: "+panel.VisionFlow); (enabled && count != 1) || (!enabled && count != 0) {
			t.Fatalf("Mihomo flow count = %d, enabled = %v", count, enabled)
		}
		encoded := h.mustDo(http.MethodGet, "/sub/"+u.SubToken+"?target=base64", nil)
		links, err := base64.StdEncoding.DecodeString(string(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if count := strings.Count(string(links), "flow="+panel.VisionFlow); (enabled && count != 1) || (!enabled && count != 0) {
			t.Fatalf("share-link flow count = %d, enabled = %v", count, enabled)
		}
	}
}

func TestInboundVisionValidationAndSyncFailure(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	var inbound model.Inbound
	h.db.First(&inbound, "remote_id = ?", 1)
	endpoint := "/api/inbounds/" + itoa(inbound.ID)
	for _, changes := range []map[string]any{{"network": "ws"}, {"network": "tcp", "disable_flow": true}, {"disable_flow": false, "missing": true}} {
		h.db.Model(&inbound).Updates(changes)
		resp, raw := h.do(http.MethodPut, endpoint, map[string]any{"vision": true})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid Vision = %d: %s", resp.StatusCode, raw)
		}
	}
	h.db.Model(&inbound).Updates(map[string]any{"missing": false, "disable_flow": false})
	h.fake.FailNext["/clients/update/"+provision.EmailFor(u)] = true
	var result struct {
		SyncError string `json:"syncError"`
	}
	raw := h.mustDo(http.MethodPut, endpoint, map[string]any{"name": inbound.Name, "vision": true})
	json.Unmarshal(raw, &result)
	if result.SyncError == "" {
		t.Fatalf("lost sync failure: %s", raw)
	}
	h.db.First(&inbound, inbound.ID)
	if inbound.Vision == nil || !*inbound.Vision {
		t.Fatal("desired Vision setting must survive a panel failure for retry")
	}
	h.mustDo(http.MethodPost, "/api/sync", nil)
	if h.fake.ClientFlow(provision.EmailFor(u), 1) != panel.VisionFlow {
		t.Fatal("sync did not recover")
	}
}

func TestInboundLegacyFlowChangesOnlyAfterExplicitChoice(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	var inbound model.Inbound
	if err := h.db.First(&inbound, "remote_id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&inbound).Update("vision", nil).Error; err != nil {
		t.Fatal(err)
	}
	h.fake.SetClientFlow(provision.EmailFor(u), 1, panel.VisionFlow)
	endpoint := "/api/inbounds/" + itoa(inbound.ID)
	for _, fields := range []map[string]any{
		{"name": "renamed"},
		{"name": "renamed again", "vision": nil},
	} {
		h.mustDo(http.MethodPut, endpoint, fields)
		h.mustDo(http.MethodPost, "/api/sync", nil)
		var saved model.Inbound
		if err := h.db.First(&saved, inbound.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Vision != nil || h.fake.ClientFlow(provision.EmailFor(u), 1) != panel.VisionFlow {
			t.Fatal("ordinary edit or rediscovery took ownership of legacy flow")
		}
	}
	// Null and false must remain different: false must trigger an immediate
	// reconcile even though both used to decode as the switch being off.
	h.mustDo(http.MethodPut, endpoint, map[string]any{"name": "renamed", "vision": false})
	var saved model.Inbound
	if err := h.db.First(&saved, inbound.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Vision == nil || *saved.Vision || h.fake.ClientFlow(provision.EmailFor(u), 1) != "" {
		t.Fatal("explicit off did not clear legacy flow immediately")
	}
	// Equal values must not become a change just because pointers differ.
	h.fake.ResetCalls()
	h.mustDo(http.MethodPut, endpoint, map[string]any{"name": "final name", "vision": false})
	for _, call := range h.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Fatalf("unchanged explicit off caused a panel write: %s", call)
		}
	}
}

func TestInboundInspectorExposesConfigurationOnlyToAdmin(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(http.MethodGet, "/api/inbounds/1", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous detail status = %d", resp.StatusCode)
	}
	h.seed()
	var rows []inboundRow
	raw := h.mustDo(http.MethodGet, "/api/inbounds", nil)
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "privateKey") || strings.Contains(string(raw), "test-private-key") {
		t.Fatal("list must not expose the configuration snapshot")
	}
	if len(rows) != 2 || !rows[0].VisionSupported || rows[0].Network != "tcp" || rows[0].Security != "reality" {
		t.Fatalf("missing transport metadata: %s", raw)
	}
	var detail struct {
		Parameters map[string]any `json:"parameters"`
		LastSeenAt int64          `json:"lastSeenAt"`
	}
	raw = h.mustDo(http.MethodGet, "/api/inbounds/"+itoa(rows[0].ID), nil)
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.LastSeenAt == 0 || detail.Parameters["streamSettings"] == nil || !strings.Contains(string(raw), "test-private-key") {
		t.Fatalf("incomplete snapshot: %s", raw)
	}
	if strings.Contains(string(raw), "clientStats") || strings.Contains(string(raw), `"clients"`) {
		t.Fatal("inspector should contain connection parameters, not user accounts")
	}
	if resp, _ := h.do(http.MethodGet, "/api/inbounds/99999", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing detail status = %d", resp.StatusCode)
	}
}
