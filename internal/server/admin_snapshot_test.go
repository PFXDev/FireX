package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/provision"
)

// Snapshots are driven the way they are used: an agent reads and submits
// through its session token, and the admin merges.

type agentSessionResponse struct {
	Session agentSessionView `json:"session"`
	Token   string           `json:"token"`
	Prompt  string           `json:"prompt"`
}

// startAgent opens a session and keeps its token for the agent helpers.
func (h *harness) startAgent() agentSessionResponse {
	h.t.Helper()
	var out agentSessionResponse
	raw := h.mustDo(http.MethodPost, "/api/agent-sessions", map[string]any{"task": "test", "ttlMinutes": 30})
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatal(err)
	}
	h.agent = out.Token
	return out
}

// agentDo calls the agent API with the session token and no cookie.
func (h *harness) agentDo(method, path string, body any) (*http.Response, []byte) {
	h.t.Helper()
	if h.agent == "" {
		h.startAgent()
	}
	reader := strings.NewReader("")
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = strings.NewReader(string(buf))
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+h.agent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func (h *harness) agentMustDo(method, path string, body any) []byte {
	h.t.Helper()
	resp, raw := h.agentDo(method, path, body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("agent %s %s = %d: %s", method, path, resp.StatusCode, raw)
	}
	return raw
}

func (h *harness) snapshot() (string, snapshotDoc) {
	h.t.Helper()
	var got struct {
		Revision string `json:"revision"`
		snapshotDoc
	}
	if err := json.Unmarshal(h.agentMustDo(http.MethodGet, "/api/agent/snapshot", nil), &got); err != nil {
		h.t.Fatal(err)
	}
	if got.Revision == "" {
		h.t.Fatal("snapshot has no revision")
	}
	return got.Revision, got.snapshotDoc
}

func (h *harness) previewSnapshot(req snapshotRequest) snapshotPreview {
	h.t.Helper()
	var out snapshotPreview
	if err := json.Unmarshal(h.agentMustDo(http.MethodPost, "/api/agent/preview", req), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) submit(req snapshotRequest) (*http.Response, []byte) {
	h.t.Helper()
	return h.agentDo(http.MethodPut, "/api/agent/submission", agentSubmission{snapshotRequest: req, Summary: "test"})
}

func (h *harness) currentSession() agentSessionView {
	h.t.Helper()
	var out struct {
		Session *agentSessionView `json:"session"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/agent-sessions/current", nil), &out)
	if out.Session == nil {
		h.t.Fatal("no live agent session")
	}
	return *out.Session
}

func (h *harness) merge(allowPanelRemoval bool) (*http.Response, agentSessionView, []byte) {
	h.t.Helper()
	sess := h.currentSession()
	resp, raw := h.do(http.MethodPost, "/api/agent-sessions/"+itoa(sess.ID)+"/merge", agentMergeRequest{
		Submissions: sess.Submissions, AllowPanelRemoval: allowPanelRemoval,
	})
	var out struct {
		Session agentSessionView `json:"session"`
	}
	json.Unmarshal(raw, &out)
	return resp, out.Session, raw
}

// applySnapshot submits and merges, then forgets the spent token.
func (h *harness) applySnapshot(req snapshotRequest, allowPanelRemoval bool) snapshotOutcome {
	h.t.Helper()
	if resp, raw := h.submit(req); resp.StatusCode != http.StatusOK {
		h.t.Fatalf("submit = %d: %s", resp.StatusCode, raw)
	}
	resp, sess, raw := h.merge(allowPanelRemoval)
	if resp.StatusCode != http.StatusOK || sess.Result == nil {
		h.t.Fatalf("merge = %d: %s", resp.StatusCode, raw)
	}
	h.agent = ""
	return *sess.Result
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

func TestSnapshotUnchangedIsEmptyAndNotSubmittable(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	h.fake.ResetCalls()

	if preview := h.previewSnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}); len(preview.Changes) != 0 {
		t.Fatalf("preview of an unchanged snapshot = %+v", preview.Changes)
	}
	if resp, raw := h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("submitting no change = %d: %s", resp.StatusCode, raw)
	}
	if again, _ := h.snapshot(); again != revision {
		t.Fatal("the revision moved without a change")
	}
	if calls := h.fake.Calls(); len(calls) != 0 {
		t.Fatalf("reading and previewing reached the panel: %v", calls)
	}
}

func TestSnapshotNeverExportsPanelToken(t *testing.T) {
	h := newHarness(t)
	h.seed()
	if raw := string(h.agentMustDo(http.MethodGet, "/api/agent/snapshot", nil)); strings.Contains(raw, `"apiToken"`) {
		t.Fatal("snapshot exposes the panel token")
	}
}

func TestSubmissionNeedsRevisionAndSummary(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	doc.NodeGroups[0].Remark = "x"

	if resp, _ := h.submit(snapshotRequest{snapshotDoc: doc}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("submit without revision = %d, want 400", resp.StatusCode)
	}
	resp, _ := h.agentDo(http.MethodPut, "/api/agent/submission", snapshotRequest{snapshotDoc: doc, Revision: revision})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("submit without summary = %d, want 400", resp.StatusCode)
	}
	// Someone edits through the UI between the agent's read and its submit.
	h.mustDo(http.MethodPut, "/api/settings/clashTemplate", map[string]any{"template": "mixed-port: 7891\n"})
	if resp, raw := h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale submit = %d, want 409: %s", resp.StatusCode, raw)
	}
}

func TestSubmissionTakesEffectOnlyOnMerge(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	name := doc.NodeGroups[0].Name
	doc.NodeGroups[0].Emoji = "🌏"

	preview := h.previewSnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision})
	if len(preview.Changes) != 1 || !hasChange(preview.Changes, "nodeGroups", name, "update") || preview.Changes[0].Fields[0].Path != "emoji" {
		t.Fatalf("preview changes = %+v", preview.Changes)
	}
	if resp, raw := h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusOK {
		t.Fatalf("submit = %d: %s", resp.StatusCode, raw)
	}
	if again, _ := h.snapshot(); again != revision {
		t.Fatal("a submission changed the configuration before review")
	}
	sess := h.currentSession()
	if sess.State != model.AgentSubmitted || sess.Summary != "test" || sess.Preview == nil || len(sess.Preview.Changes) != 1 || sess.Stale {
		t.Fatalf("review view = %+v", sess)
	}

	resp, merged, raw := h.merge(false)
	if resp.StatusCode != http.StatusOK || merged.State != model.AgentMerged || merged.Live {
		t.Fatalf("merge = %d: %s", resp.StatusCode, raw)
	}
	var group model.NodeGroup
	h.db.First(&group, "name = ?", name)
	if group.Emoji != "🌏" {
		t.Fatalf("emoji = %q after merge", group.Emoji)
	}
	if resp, _ := h.agentDo(http.MethodGet, "/api/agent", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token after merge = %d, want 401", resp.StatusCode)
	}
}

func TestAgentTokenReachesOnlyTheAgentAPI(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.startAgent()
	for _, path := range []string{"/api/overview", "/api/agent-sessions/current", "/api/panels"} {
		if resp, _ := h.agentDo(http.MethodGet, path, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("agent token on %s = %d, want 401", path, resp.StatusCode)
		}
	}
	// Nor can the agent merge its own submission.
	if resp, _ := h.agentDo(http.MethodPost, "/api/agent-sessions/1/merge", map[string]any{}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("agent merging = %d, want 401", resp.StatusCode)
	}
}

func TestAgentPromptCarriesTokenAndAddress(t *testing.T) {
	h := newHarness(t)
	h.login()
	out := h.startAgent()
	if !strings.HasPrefix(out.Token, agentTokenPrefix) || !strings.Contains(out.Prompt, out.Token) || !strings.Contains(out.Prompt, "/api/agent/guide") {
		t.Fatalf("prompt does not carry the token and guide:\n%s", out.Prompt)
	}
	if !strings.Contains(string(h.agentMustDo(http.MethodGet, "/api/agent/guide", nil)), "revision") {
		t.Fatal("guide is not served")
	}
}

func TestOnlyOneLiveSessionAndCancelEndsIt(t *testing.T) {
	h := newHarness(t)
	h.login()
	first := h.startAgent()
	if resp, _ := h.do(http.MethodPost, "/api/agent-sessions", map[string]any{"ttlMinutes": 30}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second live session = %d, want 409", resp.StatusCode)
	}
	h.mustDo(http.MethodPost, "/api/agent-sessions/"+itoa(first.Session.ID)+"/cancel", nil)
	if resp, _ := h.agentDo(http.MethodGet, "/api/agent", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token after cancel = %d, want 401", resp.StatusCode)
	}
	h.startAgent()
}

func TestExpiredSessionTokenIsRejected(t *testing.T) {
	h := newHarness(t)
	h.login()
	out := h.startAgent()
	h.db.Model(&model.AgentSession{}).Where("id = ?", out.Session.ID).Update("expires_at", nowMs()-1)
	if resp, _ := h.agentDo(http.MethodGet, "/api/agent", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired token = %d, want 401", resp.StatusCode)
	}
	var view struct {
		Session agentSessionView `json:"session"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/agent-sessions/"+itoa(out.Session.ID), nil), &view)
	if !view.Session.Expired || view.Session.Live {
		t.Fatalf("expired session view = %+v", view.Session)
	}
	// An expired session no longer blocks a new one.
	h.startAgent()
}

func TestReturnedSubmissionCanBeRevised(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	doc.NodeGroups[0].Remark = "first"
	h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision})
	sess := h.currentSession()
	h.mustDo(http.MethodPost, "/api/agent-sessions/"+itoa(sess.ID)+"/return", map[string]any{"feedback": "remark 写 second"})

	var status struct {
		State    string `json:"state"`
		Feedback string `json:"feedback"`
	}
	json.Unmarshal(h.agentMustDo(http.MethodGet, "/api/agent", nil), &status)
	if status.State != model.AgentReturned || status.Feedback != "remark 写 second" {
		t.Fatalf("agent sees %+v", status)
	}
	if resp, _, _ := h.merge(false); resp.StatusCode != http.StatusConflict {
		t.Fatalf("merging a returned submission = %d, want 409", resp.StatusCode)
	}

	doc.NodeGroups[0].Remark = "second"
	h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
	var group model.NodeGroup
	h.db.First(&group, "name = ?", doc.NodeGroups[0].Name)
	if group.Remark != "second" {
		t.Fatalf("remark = %q, want the revised submission", group.Remark)
	}
}

func TestMergeRefusesSubmissionTheReviewerHasNotSeen(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	doc.NodeGroups[0].Remark = "seen"
	h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision})
	seen := h.currentSession()

	doc.NodeGroups[0].Remark = "unseen"
	h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision})
	resp, raw := h.do(http.MethodPost, "/api/agent-sessions/"+itoa(seen.ID)+"/merge", agentMergeRequest{Submissions: seen.Submissions})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("merging with an old submission count = %d: %s", resp.StatusCode, raw)
	}
}

func TestStaleMergeReturnsToAgent(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	doc.NodeGroups[0].Remark = "agent"
	h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision})
	h.mustDo(http.MethodPut, "/api/settings/clashTemplate", map[string]any{"template": "mixed-port: 7891\n"})
	if sess := h.currentSession(); !sess.Stale {
		t.Fatal("review view should flag a submission the configuration overtook")
	}

	resp, sess, raw := h.merge(false)
	if resp.StatusCode != http.StatusConflict || sess.State != model.AgentReturned || sess.Feedback != staleFeedback {
		t.Fatalf("stale merge = %d: %s", resp.StatusCode, raw)
	}
	var group model.NodeGroup
	h.db.First(&group, "name = ?", doc.NodeGroups[0].Name)
	if group.Remark == "agent" {
		t.Fatal("a stale merge was applied")
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
	applied := h.applySnapshot(snapshotRequest{snapshotDoc: snapshotDoc{Profiles: doc.Profiles}, Revision: revision}, false)
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

func TestSnapshotInvalidDocumentIsRejectedWhole(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	profileNamed(t, doc, "标准分流").Remark = "should not stick"
	for i := range doc.Routing.Policies {
		doc.Routing.Policies[i].IsFinal = false
	}
	if resp, _ := h.agentDo(http.MethodPost, "/api/agent/preview", snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid preview = %d, want 400", resp.StatusCode)
	}
	if resp, _ := h.submit(snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid submit = %d, want 400", resp.StatusCode)
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
	if resp, raw := h.submit(req); resp.StatusCode != http.StatusOK {
		t.Fatalf("submit = %d: %s", resp.StatusCode, raw)
	}
	resp, _, raw := h.merge(false)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "确认删除") {
		t.Fatalf("unconfirmed panel removal = %d: %s", resp.StatusCode, raw)
	}
	if h.fake.Client(email) == nil {
		t.Fatal("an unconfirmed removal touched the panel")
	}

	resp, sess, raw := h.merge(true)
	if resp.StatusCode != http.StatusOK || sess.Result.RemoteCleanupFailures != 0 || h.fake.Client(email) != nil {
		t.Fatalf("confirmed merge = %d: %s; the FireX client should be gone from the panel", resp.StatusCode, raw)
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
	resp, raw := h.submit(snapshotRequest{snapshotDoc: snapshotDoc{Profiles: []snapshotProfile{}}, Revision: revision})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "套餐") {
		t.Fatalf("deleting a bound profile = %d: %s", resp.StatusCode, raw)
	}
}

func TestSnapshotLiveInboundCannotBeDropped(t *testing.T) {
	h := newHarness(t)
	h.seed()
	revision, doc := h.snapshot()
	resp, raw := h.submit(snapshotRequest{snapshotDoc: snapshotDoc{Inbounds: doc.Inbounds[:1]}, Revision: revision})
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

	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
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

	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
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
			t.Fatalf("merging a snapshot wrote an inbound to the panel: %s", call)
		}
	}

	revision, doc = h.snapshot()
	doc.Inbounds[1].ManagedConfig = nil
	h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
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
	if resp, raw := h.agentDo(http.MethodPost, "/api/agent/preview", snapshotRequest{snapshotDoc: doc, Revision: revision}); resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "apiToken") {
		t.Fatalf("new panel without token = %d: %s", resp.StatusCode, raw)
	}
	doc.Panels[len(doc.Panels)-1].APIToken = "tok2"
	applied := h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
	if len(applied.Discover) != 1 || applied.Discover[0].Panel != "p2" || applied.Discover[0].Error != "" {
		t.Fatalf("discover = %+v", applied.Discover)
	}

	// A second session can reference what discovery found.
	revision, doc = h.snapshot()
	if revision != applied.Revision {
		t.Fatal("the merge result should carry the revision after discovery")
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
	applied = h.applySnapshot(snapshotRequest{snapshotDoc: doc, Revision: revision}, false)
	if applied.ReconciledPlans != 1 || applied.SyncError != "" {
		t.Fatalf("apply = %+v, want the plan pushed", applied)
	}
	var u model.User
	h.db.First(&u, "username = ?", "carol")
	if got := second.Members(provision.EmailFor(&u)); len(got) != 1 || got[0] != 7 {
		t.Fatalf("user on p2 holds %v, want [7]", got)
	}
}
