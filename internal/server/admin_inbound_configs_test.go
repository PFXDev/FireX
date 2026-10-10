package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/provision"
)

type managedResponse struct {
	Config      *model.InboundConfig `json:"config"`
	Parameters  map[string]any       `json:"parameters"`
	State       string               `json:"state"`
	Deployments []struct {
		model.InboundDeployment
		HasBackup bool `json:"hasBackup"`
	} `json:"deployments"`
}

func managedPath(id uint) string { return "/api/inbounds/" + itoa(id) + "/config" }

func getManaged(h *harness, id uint) managedResponse {
	var result managedResponse
	json.Unmarshal(h.mustDo("GET", managedPath(id), nil), &result)
	return result
}

func adoptManaged(h *harness, id uint) managedResponse {
	var result managedResponse
	json.Unmarshal(h.mustDo("POST", managedPath(id), map[string]any{}), &result)
	return result
}

func editManaged(h *harness, id uint, managed managedResponse) managedResponse {
	var result managedResponse
	json.Unmarshal(h.mustDo("PUT", managedPath(id), map[string]any{"revision": managed.Config.Revision, "parameters": managed.Parameters}), &result)
	return result
}

type previewResponse struct {
	Mode           string `json:"mode"`
	TargetRevision string `json:"targetRevision"`
	Changes        []struct {
		Path string `json:"path"`
	} `json:"changes"`
}

func previewManaged(h *harness, id uint, rebind uint) (map[string]any, previewResponse) {
	req := map[string]any{"revision": getManaged(h, id).Config.Revision, "rebindInboundId": rebind}
	var preview previewResponse
	json.Unmarshal(h.mustDo("POST", managedPath(id)+"/preview", req), &preview)
	req["targetRevision"] = preview.TargetRevision
	return req, preview
}

func deployManaged(h *harness, id uint, req map[string]any) model.InboundDeployment {
	var result struct {
		Deployment model.InboundDeployment `json:"deployment"`
		SyncError  string                  `json:"syncError"`
	}
	json.Unmarshal(h.mustDo("POST", managedPath(id)+"/deploy", req), &result)
	if result.SyncError != "" {
		h.t.Fatalf("user sync after deployment: %s", result.SyncError)
	}
	return result.Deployment
}

func listedState(h *harness, id uint) string {
	var rows []struct {
		ID          uint   `json:"id"`
		ConfigState string `json:"configState"`
	}
	json.Unmarshal(h.mustDo("GET", "/api/inbounds", nil), &rows)
	for _, row := range rows {
		if row.ID == id {
			return row.ConfigState
		}
	}
	return "absent"
}

func TestManagedConfigAdoptEditAndDeployKeepsAccounts(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do("GET", managedPath(1), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("managed config exposed without login")
	}
	u := h.seed()
	h.fake.SetTraffic(provision.EmailFor(u), 123, 456)
	h.fake.SetClientFlow(provision.EmailFor(u), 1, "original-flow")
	if state := listedState(h, 1); state != "" {
		t.Fatalf("unmanaged inbound state = %q", state)
	}
	managed := adoptManaged(h, 1)
	if managed.State != configSynced || listedState(h, 1) != configSynced {
		t.Fatalf("adopted state = %q", managed.State)
	}
	for _, field := range []string{"id", "up", "down", "clientStats"} {
		if _, ok := managed.Parameters[field]; ok {
			t.Fatalf("managed config kept %s", field)
		}
	}
	if _, ok := managed.Parameters["settings"].(map[string]any)["clients"]; ok {
		t.Fatal("managed config kept user accounts")
	}
	if resp, _ := h.do("POST", managedPath(1), map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatal("adopted twice")
	}

	managed.Parameters["listen"] = "127.0.0.1"
	managed.Parameters["port"] = float64(9443)
	managed.Parameters["futureOption"] = map[string]any{"value": float64(42)}
	managed.Parameters["streamSettings"].(map[string]any)["tcpSettings"] = map[string]any{"acceptProxyProtocol": true}
	managed = editManaged(h, 1, managed)
	if managed.State != configPending || managed.Config.Revision != 2 {
		t.Fatalf("edited state = %q revision %d", managed.State, managed.Config.Revision)
	}
	req, preview := previewManaged(h, 1, 0)
	if preview.Mode != "update" || len(preview.Changes) == 0 {
		t.Fatalf("preview: %+v", preview)
	}
	record := deployManaged(h, 1, req)
	if record.Status != "verified" {
		t.Fatalf("deployment: %+v", record)
	}
	cl := h.fake.Client(provision.EmailFor(u))
	if cl.Up != 123 || cl.Down != 456 || cl.ID != u.UUID || h.fake.ClientFlow(provision.EmailFor(u), 1) != "original-flow" {
		t.Fatal("deployment overwrote client state")
	}
	var inbound model.Inbound
	h.db.First(&inbound, 1)
	if inbound.Port != 9443 || inbound.Listen != "127.0.0.1" || !strings.Contains(inbound.Config, "futureOption") {
		t.Fatalf("readback lost configuration: %s", inbound.Config)
	}
	if state := getManaged(h, 1).State; state != configSynced {
		t.Fatalf("deployed state = %q", state)
	}
	h.db.First(&record, record.ID)
	if !strings.Contains(record.BeforeConfig, `"port":443`) || strings.Contains(record.BeforeConfig, u.UUID) || strings.Contains(record.DesiredConfig, "clients") {
		t.Fatal("incorrect backup")
	}
	if raw := h.mustDo("GET", "/api/inbounds", nil); strings.Contains(string(raw), "test-private-key") {
		t.Fatal("inbound list leaked keys")
	}
}

func TestManagedConfigReportsDriftAndRestoresIt(t *testing.T) {
	h := newHarness(t)
	h.seed()
	adoptManaged(h, 1)
	// The panel database is overwritten with a different configuration.
	h.fake.RemoveInbound(1)
	h.fake.AddInbound(paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "overwritten", Enable: true,
		StreamSettings: `{"network":"tcp","security":"reality","realitySettings":{"serverNames":["wrong.example.com"],"privateKey":"wrong-key","shortIds":["cd"]}}`})
	h.mustDo("POST", "/api/panels/1/discover", nil)
	if state := listedState(h, 1); state != configDrifted {
		t.Fatalf("drift state = %q", state)
	}
	req, preview := previewManaged(h, 1, 0)
	if preview.Mode != "update" {
		t.Fatalf("preview: %+v", preview)
	}
	if record := deployManaged(h, 1, req); record.Status != "verified" {
		t.Fatalf("restore: %+v", record)
	}
	var inbound model.Inbound
	h.db.First(&inbound, 1)
	if !strings.Contains(inbound.Config, "test-private-key") || strings.Contains(inbound.Config, "wrong-key") || inbound.RemoteRemark != "hk" {
		t.Fatalf("panel not restored: %s", inbound.Config)
	}
	if state := listedState(h, 1); state != configSynced {
		t.Fatalf("restored state = %q", state)
	}
}

func TestManagedConfigRecreatesMissingInboundWithIdentity(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	adoptManaged(h, 1)
	h.fake.RemoveInbound(1)
	h.mustDo("POST", "/api/panels/1/discover", nil)
	if state := listedState(h, 1); state != configMissing {
		t.Fatalf("missing state = %q", state)
	}
	req, preview := previewManaged(h, 1, 0)
	if preview.Mode != "create" {
		t.Fatalf("preview: %+v", preview)
	}
	record := deployManaged(h, 1, req)
	if record.Status != "verified" || record.InboundID != 1 || record.RemoteID == 1 {
		t.Fatalf("recreate: %+v", record)
	}
	var inbound model.Inbound
	h.db.First(&inbound, 1)
	var links int64
	h.db.Model(&model.NodeGroupInbound{}).Where("inbound_id = ?", 1).Count(&links)
	if inbound.Missing || !inbound.Enabled || links != 1 || inbound.RemoteID != record.RemoteID {
		t.Fatal("recreate lost identity, enablement or grouping")
	}
	if !slices.Contains(h.fake.Members(provision.EmailFor(u)), record.RemoteID) {
		t.Fatal("users not attached to the recreated inbound")
	}
	var rows int64
	h.db.Model(&model.Inbound{}).Count(&rows)
	h.mustDo("POST", "/api/panels/1/discover", nil)
	var after int64
	h.db.Model(&model.Inbound{}).Count(&after)
	if after != rows || listedState(h, 1) != configSynced {
		t.Fatal("discovery duplicated the recreated inbound")
	}
}

func TestManagedConfigRebindsRenumberedInbound(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	adoptManaged(h, 1)
	// An overwritten panel database renumbers the inbound and loses its keys.
	h.fake.RemoveInbound(1)
	h.fake.AddInbound(paneltest.Inbound{ID: 7, Port: 443, Protocol: "vless", Remark: "renumbered", Enable: true,
		StreamSettings: `{"network":"tcp","security":"reality","realitySettings":{"privateKey":"wrong-key"}}`})
	h.mustDo("POST", "/api/panels/1/discover", nil)
	var duplicate model.Inbound
	h.db.First(&duplicate, "panel_id = ? AND remote_id = ?", 1, 7)
	if resp, raw := h.do("POST", managedPath(1)+"/preview", map[string]any{"revision": 1}); resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), "重新绑定") {
		t.Fatalf("create over a renumbered inbound: %d %s", resp.StatusCode, raw)
	}
	req, preview := previewManaged(h, 1, duplicate.ID)
	if preview.Mode != "rebind" {
		t.Fatalf("preview: %+v", preview)
	}
	record := deployManaged(h, 1, req)
	if record.Status != "verified" || record.RemoteID != 7 {
		t.Fatalf("rebind: %+v", record)
	}
	var inbound model.Inbound
	h.db.First(&inbound, 1)
	var links, leftover int64
	h.db.Model(&model.NodeGroupInbound{}).Where("inbound_id = ?", 1).Count(&links)
	h.db.Model(&model.Inbound{}).Where("id = ?", duplicate.ID).Count(&leftover)
	if inbound.RemoteID != 7 || inbound.Missing || links != 1 || leftover != 0 || !strings.Contains(inbound.Config, "test-private-key") {
		t.Fatal("rebind lost identity or configuration")
	}
	if !slices.Contains(h.fake.Members(provision.EmailFor(u)), 7) {
		t.Fatal("users not attached after rebind")
	}
}

func TestManagedDeploymentRejectsStalePreviewAndNeverRetries(t *testing.T) {
	h := newHarness(t)
	h.seed()
	managed := adoptManaged(h, 1)
	managed.Parameters["port"] = float64(9443)
	editManaged(h, 1, managed)
	req, _ := previewManaged(h, 1, 0)
	h.fake.RemoveInbound(2)
	h.fake.AddInbound(paneltest.Inbound{ID: 2, Port: 8444, Protocol: "vless", Remark: "jp", Enable: true})
	h.fake.ResetCalls()
	if resp, raw := h.do("POST", managedPath(1)+"/deploy", req); resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale preview: %d %s", resp.StatusCode, raw)
	}
	for _, call := range h.fake.Calls() {
		if strings.HasPrefix(call, "POST /inbounds") {
			t.Fatal("stale preview wrote panel")
		}
	}
	managed = getManaged(h, 1)
	managed.Parameters["port"] = float64(8444)
	editManaged(h, 1, managed)
	if resp, _ := h.do("POST", managedPath(1)+"/preview", map[string]any{"revision": 3}); resp.StatusCode != http.StatusConflict {
		t.Fatal("port conflict accepted")
	}
	managed = getManaged(h, 1)
	managed.Parameters["port"] = float64(9443)
	editManaged(h, 1, managed)
	req, _ = previewManaged(h, 1, 0)
	h.fake.FailNext["/inbounds/update/1"] = true
	h.fake.ResetCalls()
	record := deployManaged(h, 1, req)
	if record.Status != "unverified" || record.Error == "" {
		t.Fatal("failed write claimed success")
	}
	count := 0
	for _, call := range h.fake.Calls() {
		if call == "POST /inbounds/update/1" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("mutation retried")
	}
	h.db.First(&record, record.ID)
	if record.BeforeConfig == "" || getManaged(h, 1).State != configPending {
		t.Fatal("failed write lost backup or claimed sync")
	}
}

func TestManagedConfigRestoresFromBackupAndValidates(t *testing.T) {
	h := newHarness(t)
	h.seed()
	managed := adoptManaged(h, 1)
	for _, value := range []any{nil, []any{}, map[string]any{"port": 65536}, map[string]any{"port": 12.5}} {
		resp, _ := h.do("PUT", managedPath(1), map[string]any{"revision": managed.Config.Revision, "parameters": value})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid configuration accepted: %v (%d)", value, resp.StatusCode)
		}
	}
	managed.Parameters["port"] = float64(9443)
	managed = editManaged(h, 1, managed)
	if resp, _ := h.do("PUT", managedPath(1), map[string]any{"revision": 1, "parameters": managed.Parameters}); resp.StatusCode != http.StatusConflict {
		t.Fatal("stale revision accepted")
	}
	req, _ := previewManaged(h, 1, 0)
	record := deployManaged(h, 1, req)
	// Restoring a before image is a new revision, deployed like any edit.
	var restored managedResponse
	json.Unmarshal(h.mustDo("PUT", managedPath(1), map[string]any{"revision": managed.Config.Revision, "deploymentId": record.ID}), &restored)
	if restored.Parameters["port"] != float64(443) || restored.State != configPending {
		t.Fatalf("backup not restored: %+v", restored.Parameters["port"])
	}
	// After release, the same backup can be adopted for a vanished inbound.
	h.mustDo("DELETE", managedPath(1), nil)
	h.fake.RemoveInbound(1)
	h.mustDo("POST", "/api/panels/1/discover", nil)
	if resp, _ := h.do("POST", managedPath(1), map[string]any{}); resp.StatusCode != http.StatusBadRequest {
		t.Fatal("adopted a vanished inbound's stale discovery")
	}
	var adopted managedResponse
	json.Unmarshal(h.mustDo("POST", managedPath(1), map[string]any{"deploymentId": record.ID}), &adopted)
	if adopted.State != configMissing || adopted.Parameters["port"] != float64(443) || len(adopted.Deployments) != 1 || !adopted.Deployments[0].HasBackup {
		t.Fatalf("backup adoption: %+v", adopted)
	}
	if resp, _ := h.do("PUT", managedPath(2), map[string]any{"revision": 0, "deploymentId": record.ID}); resp.StatusCode != http.StatusBadRequest {
		t.Fatal("another inbound's backup accepted")
	}
}

func TestManagedDeploymentUsesSeparateEnableEndpoint(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.fake.KeepEnableOnUpdate = true
	managed := adoptManaged(h, 1)
	managed.Parameters["enable"] = false
	editManaged(h, 1, managed)
	req, _ := previewManaged(h, 1, 0)
	if record := deployManaged(h, 1, req); record.Status != "verified" {
		t.Fatalf("separate enable endpoint: %+v", record)
	}
	if !slices.Contains(h.fake.Calls(), "POST /inbounds/setEnable/1") {
		t.Fatal("did not use dedicated enable endpoint")
	}
}

func TestTemplateCreatesManagedInboundPerNode(t *testing.T) {
	h := newHarness(t)
	h.seed()
	var template templateResponse
	json.Unmarshal(h.mustDo("POST", "/api/inbound-templates", map[string]any{"name": "REALITY", "inboundId": 1}), &template)
	for _, field := range []string{"listen", "port", "tag", "remark"} {
		if _, ok := template.Parameters[field]; ok {
			t.Fatalf("template kept node field %s", field)
		}
	}
	reality := template.Parameters["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	if reality["privateKey"] != "test-private-key" {
		t.Fatal("template lost shared keys")
	}
	var result struct {
		Deployment model.InboundDeployment `json:"deployment"`
	}
	json.Unmarshal(h.mustDo("POST", "/api/inbound-templates/"+itoa(template.Template.ID)+"/instantiate", map[string]any{
		"revision": template.Template.Revision, "panelId": 1, "listen": "127.0.0.1", "port": 9443, "remark": "proxied",
		"acceptProxyProtocol": true, "publicAddress": "proxy.example.com", "publicPort": 443,
	}), &result)
	record := result.Deployment
	if record.Status != "verified" || record.InboundID == 0 || record.TemplateID != template.Template.ID {
		t.Fatalf("instantiate: %+v", record)
	}
	var inbound model.Inbound
	h.db.First(&inbound, record.InboundID)
	var links int64
	h.db.Model(&model.NodeGroupInbound{}).Where("inbound_id = ?", inbound.ID).Count(&links)
	if inbound.Enabled || links != 0 || inbound.Port != 9443 || inbound.PublicAddress != "proxy.example.com" || inbound.PublicPort != 443 {
		t.Fatalf("new inbound: %+v", inbound)
	}
	if !strings.Contains(inbound.Config, `"acceptProxyProtocol":true`) {
		t.Fatal("PROXY setting not applied")
	}
	if state := listedState(h, inbound.ID); state != configSynced {
		t.Fatalf("instantiated state = %q", state)
	}
	if resp, _ := h.do("POST", "/api/inbound-templates/"+itoa(template.Template.ID)+"/instantiate", map[string]any{
		"revision": template.Template.Revision, "panelId": 1, "port": 9443, "listen": "127.0.0.1",
	}); resp.StatusCode != http.StatusConflict {
		t.Fatal("port conflict accepted")
	}
	// Later template edits leave the created inbound alone.
	reality["privateKey"] = "rotated"
	h.mustDo("PUT", "/api/inbound-templates/"+itoa(template.Template.ID), map[string]any{"revision": 1, "parameters": template.Parameters})
	managed := getManaged(h, inbound.ID)
	if managed.Parameters["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["privateKey"] != "test-private-key" {
		t.Fatal("template edit changed a created inbound")
	}
	h.mustDo("DELETE", "/api/inbound-templates/"+itoa(template.Template.ID), nil)
	if resp, _ := h.do("GET", "/api/inbound-templates/"+itoa(template.Template.ID), nil); resp.StatusCode != http.StatusNotFound {
		t.Fatal("template not deleted")
	}
}
