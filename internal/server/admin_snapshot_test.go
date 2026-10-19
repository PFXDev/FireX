package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/provision"
)

type snapshotApplied struct {
	Revision              string              `json:"revision"`
	Changes               []snapshotChange    `json:"changes"`
	Discover              []snapshotDiscovery `json:"discover"`
	ReconciledPlans       int                 `json:"reconciledPlans"`
	SyncError             string              `json:"syncError"`
	RemoteCleanupFailures int                 `json:"remoteCleanupFailures"`
	PendingDeployments    []pendingDeployment `json:"pendingDeployments"`
}

type snapshotPreviewed struct {
	Changes        []snapshotChange `json:"changes"`
	Discover       []string         `json:"discover"`
	RemovedPanels  []string         `json:"removedPanels"`
	ReconcilePlans int              `json:"reconcilePlans"`
}

func (h *harness) snapshot() (string, snapshotDoc) {
	h.t.Helper()
	var got struct {
		Revision string `json:"revision"`
		snapshotDoc
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/snapshot", nil), &got); err != nil {
		h.t.Fatal(err)
	}
	if got.Revision == "" {
		h.t.Fatal("snapshot has no revision")
	}
	return got.Revision, got.snapshotDoc
}

func (h *harness) applySnapshot(req snapshotRequest) snapshotApplied {
	h.t.Helper()
	var out snapshotApplied
	if err := json.Unmarshal(h.mustDo(http.MethodPut, "/api/snapshot", req), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) previewSnapshot(req snapshotRequest) snapshotPreviewed {
	h.t.Helper()
	var out snapshotPreviewed
	if err := json.Unmarshal(h.mustDo(http.MethodPost, "/api/snapshot/preview", req), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func profileNamed(t *testing.T, doc snapshotDoc, name string) *snapshotProfile {
	t.Helper()
	for i := range doc.Profiles {
		if doc.Profiles[i].Name == name {
			return &doc.Profiles[i]
		}
	}
	t.Fatalf("snapshot has no profile %q", name)
	return nil
}

func hasChange(changes []snapshotChange, section, key, action string) bool {
	for _, c := range changes {
		if c.Section == section && c.Key == key && c.Action == action {
			return true
		}
	}
	return false
}

func TestSnapshotRoundTripIsANoOp(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	h.fake.ResetCalls()

	if preview := h.previewSnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}); len(preview.Changes) != 0 {
		t.Fatalf("preview of an unchanged snapshot = %+v", preview.Changes)
	}
	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if len(applied.Changes) != 0 || applied.ReconciledPlans != 0 {
		t.Fatalf("applying an unchanged snapshot = %+v", applied)
	}
	if applied.Revision != revision {
		t.Fatalf("revision moved from %s to %s", revision, applied.Revision)
	}
	if calls := h.fake.Calls(); len(calls) != 0 {
		t.Fatalf("an unchanged snapshot reached the panel: %v", calls)
	}
}

func TestSnapshotNeverExportsPanelToken(t *testing.T) {
	h := newHarness(t)
	h.seed()
	raw := string(h.mustDo(http.MethodGet, "/api/snapshot", nil))
	if strings.Contains(raw, `"apiToken"`) {
		t.Fatal("snapshot exposes the panel token")
	}
}

func TestSnapshotRequiresCurrentRevision(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()

	if resp, _ := h.do(http.MethodPut, "/api/snapshot", snapshotRequest{snapshotDoc: doc}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("apply without revision = %d, want 400", resp.StatusCode)
	}
	// Someone edits through the UI between the agent's read and its write.
	h.mustDo(http.MethodPut, "/api/settings/clashTemplate", map[string]any{"template": "mixed-port: 7891\n"})
	if resp, raw := h.do(http.MethodPut, "/api/snapshot", snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale apply = %d, want 409: %s", resp.StatusCode, raw)
	}
}

func TestSnapshotPreviewWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	name := doc.NodeGroups[0].Name
	doc.NodeGroups[0].Emoji = "🌏"

	preview := h.previewSnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if len(preview.Changes) != 1 || !hasChange(preview.Changes, "nodeGroups", name, "update") || preview.Changes[0].Fields[0].Path != "emoji" {
		t.Fatalf("preview changes = %+v", preview.Changes)
	}
	if again, _ := h.snapshot(); again != revision {
		t.Fatal("a preview changed the stored configuration")
	}
}

func TestSnapshotSectionNarrowsProfileAndPushesPanels(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	email := provision.EmailFor(u)
	if got := h.fake.Members(email); len(got) != 2 {
		t.Fatalf("seeded user holds %v, want two inbounds", got)
	}
	revision, doc := h.snapshot()
	profile := profileNamed(t, doc, "标准分流")
	profile.NodeGroups = profile.NodeGroups[:1]

	// Only the profiles section: every other section is left as stored.
	applied := h.applySnapshot(snapshotRequest{snapshotDoc: snapshotDoc{Profiles: doc.Profiles}, Revision: revision})
	if applied.ReconciledPlans != 1 || applied.SyncError != "" {
		t.Fatalf("apply = %+v, want one plan reconciled", applied)
	}
	if got := h.fake.Members(email); len(got) != 1 {
		t.Fatalf("after narrowing the user holds %v, want one inbound", got)
	}
	var groups int64
	h.db.Model(&model.NodeGroup{}).Count(&groups)
	if groups != 2 {
		t.Fatalf("node groups = %d; an omitted section must stay untouched", groups)
	}
}

func TestSnapshotInvalidDocumentRollsBackEverySection(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	profileNamed(t, doc, "标准分流").Remark = "should not stick"
	for i := range doc.Routing.Policies {
		doc.Routing.Policies[i].IsFinal = false
	}
	if resp, _ := h.do(http.MethodPut, "/api/snapshot", snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid snapshot = %d, want 400", resp.StatusCode)
	}
	if again, _ := h.snapshot(); again != revision {
		t.Fatal("a rejected snapshot left changes behind")
	}
}

func TestSnapshotPanelRemovalNeedsConfirmation(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	email := provision.EmailFor(u)
	revision, _ := h.snapshot()
	req := snapshotRequest{snapshotDoc: snapshotDoc{Panels: []snapshotPanel{}}, Revision: revision}

	if preview := h.previewSnapshot(req); len(preview.RemovedPanels) != 1 || !hasChange(preview.Changes, "panels", "p1", "delete") {
		t.Fatalf("preview = %+v, want p1 removed", preview)
	}
	resp, raw := h.do(http.MethodPut, "/api/snapshot", req)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "allowPanelRemoval") {
		t.Fatalf("unconfirmed panel removal = %d: %s", resp.StatusCode, raw)
	}
	if h.fake.Client(email) == nil {
		t.Fatal("an unconfirmed removal touched the panel")
	}

	req.AllowPanelRemoval = true
	applied := h.applySnapshot(req)
	if applied.RemoteCleanupFailures != 0 || h.fake.Client(email) != nil {
		t.Fatalf("apply = %+v; the FireX client should be gone from the panel", applied)
	}
	var panels, inbounds, links int64
	h.db.Model(&model.Panel{}).Count(&panels)
	h.db.Model(&model.Inbound{}).Count(&inbounds)
	h.db.Model(&model.NodeGroupInbound{}).Count(&links)
	if panels+inbounds+links != 0 {
		t.Fatalf("left behind %d panels, %d inbounds, %d memberships", panels, inbounds, links)
	}
}

func TestSnapshotKeepsProfileBoundByPlan(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, _ := h.snapshot()
	resp, raw := h.do(http.MethodPut, "/api/snapshot", snapshotRequest{snapshotDoc: snapshotDoc{Profiles: []snapshotProfile{}}, Revision: revision})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "套餐") {
		t.Fatalf("deleting a bound profile = %d: %s", resp.StatusCode, raw)
	}
}

func TestSnapshotLiveInboundCannotBeDropped(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	resp, raw := h.do(http.MethodPut, "/api/snapshot", snapshotRequest{snapshotDoc: snapshotDoc{Inbounds: doc.Inbounds[:1]}, Revision: revision})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "p1#2") {
		t.Fatalf("dropping a live inbound = %d: %s", resp.StatusCode, raw)
	}
}

func TestSnapshotStoresDisabledRowsAsWritten(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	doc.NodeGroups = append(doc.NodeGroups, snapshotNodeGroup{Name: "备用", Enabled: new(false)})
	doc.Profiles = append(doc.Profiles, snapshotProfile{Name: "停用方案", Enabled: new(false)})
	doc.Routing.Policies = append([]snapshotPolicy{{Name: "停用策略", Enabled: new(false)}}, doc.Routing.Policies...)

	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	for _, want := range []struct{ section, key string }{{"nodeGroups", "备用"}, {"profiles", "停用方案"}, {"policies", "停用策略"}} {
		if !hasChange(applied.Changes, want.section, want.key, "create") {
			t.Fatalf("changes = %+v, want %s %s created", applied.Changes, want.section, want.key)
		}
	}
	var group model.NodeGroup
	var profile model.Profile
	var policy model.Policy
	h.db.First(&group, "name = ?", "备用")
	h.db.First(&profile, "name = ?", "停用方案")
	h.db.First(&policy, "name = ?", "停用策略")
	if group.Enabled || profile.Enabled || policy.Enabled {
		t.Fatalf("enabled: group %v, profile %v, policy %v; want all false", group.Enabled, profile.Enabled, policy.Enabled)
	}
}

func TestSnapshotManagedConfigStaysPendingUntilDeployed(t *testing.T) {
	h := newHarness(t)
	h.seed()
	var inbounds []model.Inbound
	h.db.Order("remote_id ASC").Find(&inbounds)
	first, second := inbounds[0], inbounds[1]
	adoptManaged(h, first.ID)

	var detail struct {
		Parameters map[string]any `json:"parameters"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/inbounds/"+itoa(second.ID), nil), &detail)

	revision, doc := h.snapshot()
	if doc.Inbounds[0].ManagedConfig == nil || doc.Inbounds[1].ManagedConfig != nil {
		t.Fatal("snapshot should carry exactly the managed configuration")
	}
	doc.Inbounds[0].ManagedConfig["remark"] = "edited by agent"
	// Taking over what the panel already runs is in sync straight away.
	doc.Inbounds[1].ManagedConfig = detail.Parameters
	h.fake.ResetCalls()

	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if len(applied.PendingDeployments) != 1 || applied.PendingDeployments[0].InboundID != first.ID || applied.PendingDeployments[0].Revision != 2 {
		t.Fatalf("pending deployments = %+v, want only the edited inbound at revision 2", applied.PendingDeployments)
	}
	if state := listedState(h, first.ID); state != configPending {
		t.Fatalf("edited inbound state = %q, want pending", state)
	}
	if state := listedState(h, second.ID); state != configSynced {
		t.Fatalf("adopted inbound state = %q, want synced", state)
	}
	for _, call := range h.fake.Calls() {
		if strings.Contains(call, "inbounds/update") || strings.Contains(call, "inbounds/add") {
			t.Fatalf("applying a snapshot wrote an inbound to the panel: %s", call)
		}
	}

	revision, doc = h.snapshot()
	doc.Inbounds[1].ManagedConfig = nil
	h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if state := listedState(h, second.ID); state != "" {
		t.Fatalf("released inbound state = %q, want unmanaged", state)
	}
}

func TestSnapshotAddsPanelThenGroupsItsInbounds(t *testing.T) {
	h := newHarness(t)
	h.seed()
	second := paneltest.New("tok2", paneltest.Inbound{ID: 7, Port: 9443, Protocol: "vless", Remark: "sg", Enable: true})
	t.Cleanup(second.Close)

	revision, doc := h.snapshot()
	doc.Panels = append(doc.Panels, snapshotPanel{Name: "p2", BaseURL: second.URL()})
	if resp, raw := h.do(http.MethodPost, "/api/snapshot/preview", snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "apiToken") {
		t.Fatalf("new panel without token = %d: %s", resp.StatusCode, raw)
	}
	doc.Panels[len(doc.Panels)-1].APIToken = "tok2"
	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if len(applied.Discover) != 1 || applied.Discover[0].Panel != "p2" || applied.Discover[0].Error != "" {
		t.Fatalf("discover = %+v", applied.Discover)
	}

	// The second pass can reference what discovery found.
	revision, doc = h.snapshot()
	if revision != applied.Revision {
		t.Fatal("the apply response should carry the revision after discovery")
	}
	found := false
	for i := range doc.Inbounds {
		if doc.Inbounds[i].Panel == "p2" && doc.Inbounds[i].RemoteID == 7 {
			doc.Inbounds[i].Enabled = new(true)
			found = true
		}
	}
	if !found {
		t.Fatal("discovered inbound p2#7 is not in the snapshot")
	}
	doc.NodeGroups = append(doc.NodeGroups, snapshotNodeGroup{Name: "🇸🇬 新加坡", Inbounds: []inboundRef{{Panel: "p2", RemoteID: 7}}})
	profile := profileNamed(t, doc, "标准分流")
	profile.NodeGroups = append(profile.NodeGroups, "🇸🇬 新加坡")
	applied = h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if applied.ReconciledPlans != 1 || applied.SyncError != "" {
		t.Fatalf("apply = %+v, want the plan pushed", applied)
	}
	var u model.User
	h.db.First(&u, "username = ?", "carol")
	if got := second.Members(provision.EmailFor(&u)); len(got) != 1 || got[0] != 7 {
		t.Fatalf("user on p2 holds %v, want [7]", got)
	}
}
