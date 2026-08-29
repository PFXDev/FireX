package provision

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/store"
)

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// fixture wires a manager to one fake panel with two inbounds.
type fixture struct {
	db    *store.DB
	mgr   *Manager
	fake  *paneltest.Panel
	panel *model.Panel
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fake := paneltest.New("tok",
		paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "hk-reality", Tag: "in-443", Enable: true},
		paneltest.Inbound{ID: 2, Port: 8443, Protocol: "vless", Remark: "jp-reality", Tag: "in-8443", Enable: true},
	)
	t.Cleanup(fake.Close)

	db := testDB(t)
	p := model.Panel{Name: "p1", BaseURL: fake.URL(), APIToken: "tok", Enabled: true}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("create panel: %v", err)
	}
	return &fixture{db: db, mgr: NewManager(db), fake: fake, panel: &p}
}

// enableNodes turns on every discovered node and returns their FireX ids in
// inbound order.
func (f *fixture) enableNodes(t *testing.T) []uint {
	t.Helper()
	var nodes []model.Node
	if err := f.db.Order("inbound_id ASC").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	ids := make([]uint, 0, len(nodes))
	for i := range nodes {
		nodes[i].Enabled = true
		if err := f.db.Save(&nodes[i]).Error; err != nil {
			t.Fatalf("enable node: %v", err)
		}
		ids = append(ids, nodes[i].ID)
	}
	return ids
}

func (f *fixture) newPlan(t *testing.T, nodeIDs []uint) *model.Plan {
	t.Helper()
	plan := model.Plan{Name: "basic", TrafficBytes: 1000, DeviceLimit: 3, Enabled: true}
	if err := f.db.Create(&plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	for _, id := range nodeIDs {
		if err := f.db.Create(&model.PlanNode{PlanID: plan.ID, NodeID: id}).Error; err != nil {
			t.Fatalf("link plan node: %v", err)
		}
	}
	return &plan
}

func (f *fixture) newUser(t *testing.T, planID uint) *model.User {
	t.Helper()
	u := model.User{
		Username: "alice", UUID: "uuid-alice", SubToken: "subtok",
		PlanID: planID, Enabled: true, TrafficLimit: 1000,
	}
	if err := f.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return &u
}

func TestDiscoverCreatesDisabledNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var nodes []model.Node
	f.db.Order("inbound_id ASC").Find(&nodes)
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}
	// A newly discovered inbound must not silently reach every subscription.
	for _, n := range nodes {
		if n.Enabled {
			t.Errorf("node %d discovered already enabled", n.InboundID)
		}
	}
	if nodes[0].Port != 443 || nodes[0].Protocol != "vless" || nodes[0].RemoteRemark != "hk-reality" {
		t.Errorf("node 1 = %+v", nodes[0])
	}
	if f.panel.Status != model.PanelStatusOnline || f.panel.XrayVersion != "25.1.1" {
		t.Errorf("panel health = %q/%q", f.panel.Status, f.panel.XrayVersion)
	}
}

func TestDiscoverPreservesAdminFieldsAndFlagsMissing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var node model.Node
	f.db.First(&node, "inbound_id = ?", 2)
	node.Enabled = true
	node.Name = "Tokyo 01"
	node.Region = "JP"
	f.db.Save(&node)

	f.fake.RemoveInbound(2)
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var after model.Node
	f.db.First(&after, node.ID)
	if !after.Missing {
		t.Error("Missing = false after the inbound disappeared upstream")
	}
	// Deleting the row would lose the plan membership an admin curated.
	if after.Name != "Tokyo 01" || after.Region != "JP" {
		t.Errorf("admin fields lost: %+v", after)
	}

	f.fake.AddInbound(paneltest.Inbound{ID: 2, Port: 8443, Protocol: "vless", Remark: "jp-reality", Enable: true})
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}
	f.db.First(&after, node.ID)
	if after.Missing {
		t.Error("Missing stayed true after the inbound came back")
	}
}

func TestDiscoverMarksPanelOffline(t *testing.T) {
	f := newFixture(t)
	f.fake.Close()
	if err := f.mgr.DiscoverPanel(context.Background(), f.panel); err == nil {
		t.Fatal("DiscoverPanel() error = nil, want a connection error")
	}
	var p model.Panel
	f.db.First(&p, f.panel.ID)
	if p.Status != model.PanelStatusOffline || p.LastError == "" {
		t.Errorf("panel = %q / %q, want offline with a reason", p.Status, p.LastError)
	}
}

func TestReconcileCreatesClientOnPlanNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}
	nodeIDs := f.enableNodes(t)
	// Only the first node is in the plan.
	plan := f.newPlan(t, nodeIDs[:1])
	u := f.newUser(t, plan.ID)

	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}

	client := f.fake.Client("alice@firex")
	if client == nil {
		t.Fatal("client was not created on the panel")
	}
	if client.ID != "uuid-alice" || !client.Enable || client.TotalGB != 1000 || client.LimitIP != 3 {
		t.Errorf("client = %+v", client)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 1 || got[0] != 1 {
		t.Errorf("members = %v, want [1]", got)
	}

	var rec model.UserPanel
	if err := f.db.First(&rec, "user_id = ?", u.ID).Error; err != nil {
		t.Fatalf("no UserPanel record: %v", err)
	}
	if rec.State != model.SyncStateSynced || rec.InboundIDs != "1" {
		t.Errorf("record = %+v", rec)
	}
}

func TestReconcileAttachesAndDetachesOnPlanChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs[:1])
	u := f.newUser(t, plan.ID)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}

	// Widen the plan to both nodes.
	f.db.Create(&model.PlanNode{PlanID: plan.ID, NodeID: nodeIDs[1]})
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 2 {
		t.Fatalf("members = %v, want both inbounds", got)
	}

	// Narrow it back to the second node only.
	f.db.Where("plan_id = ? AND node_id = ?", plan.ID, nodeIDs[0]).Delete(&model.PlanNode{})
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 1 || got[0] != 2 {
		t.Errorf("members = %v, want [2]", got)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}

	f.fake.ResetCalls()
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("second ReconcileUser() error = %v", err)
	}
	// A converged user must cost one read and no writes; a needless update
	// makes the panel re-check its xray config on every sync tick.
	for _, call := range f.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Errorf("unnecessary write on a converged user: %s (all calls: %v)", call, f.fake.Calls())
		}
	}
}

func TestReconcileDisablesInactiveUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, u)

	u.Enabled = false
	f.db.Save(u)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	client := f.fake.Client("alice@firex")
	if client == nil || client.Enable {
		t.Fatalf("client = %+v, want enable=false", client)
	}
	// Disabling must not delete the client, or its traffic history is lost.
	if got := f.fake.Members("alice@firex"); len(got) != 2 {
		t.Errorf("members = %v, want the client kept on both inbounds", got)
	}
}

func TestReconcileRemovesClientWhenPlanCleared(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, u)

	u.PlanID = 0
	f.db.Save(u)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if c := f.fake.Client("alice@firex"); c != nil {
		t.Errorf("client %+v still on the panel after losing every node", c)
	}
	var count int64
	f.db.Model(&model.UserPanel{}).Where("user_id = ?", u.ID).Count(&count)
	if count != 0 {
		t.Errorf("UserPanel rows = %d, want 0", count)
	}
}

func TestReconcileRecordsPanelFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)

	f.fake.FailNext["/clients/add"] = true
	if err := f.mgr.ReconcileUser(ctx, u); err == nil {
		t.Fatal("ReconcileUser() error = nil, want the panel's failure surfaced")
	}
	var rec model.UserPanel
	f.db.First(&rec, "user_id = ?", u.ID)
	if rec.State != model.SyncStateFailed || rec.LastError == "" {
		t.Errorf("record = %+v, want a failed state carrying the reason", rec)
	}

	// The next pass must recover on its own.
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("recovery ReconcileUser() error = %v", err)
	}
	if f.fake.Client("alice@firex") == nil {
		t.Error("client was not created on the retry")
	}
}

func TestCollectTrafficAccumulatesDeltas(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, u)

	f.fake.SetTraffic("alice@firex", 100, 200)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	var got model.User
	f.db.First(&got, u.ID)
	if got.Upload != 100 || got.Download != 200 {
		t.Fatalf("traffic = %d/%d, want 100/200", got.Upload, got.Download)
	}

	// A second poll must add only the delta, not the whole counter again.
	f.fake.SetTraffic("alice@firex", 150, 260)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	f.db.First(&got, u.ID)
	if got.Upload != 150 || got.Download != 260 {
		t.Errorf("traffic = %d/%d, want 150/260", got.Upload, got.Download)
	}
}

func TestCollectTrafficHandlesPanelSideReset(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, u)

	f.fake.SetTraffic("alice@firex", 400, 400)
	f.mgr.CollectTraffic(ctx)

	// Someone reset the counters on the panel: the counter goes backwards, and
	// the new reading is the whole delta rather than a negative one.
	f.fake.SetTraffic("alice@firex", 10, 5)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	var got model.User
	f.db.First(&got, u.ID)
	if got.Upload != 410 || got.Download != 405 {
		t.Errorf("traffic = %d/%d, want 410/405", got.Upload, got.Download)
	}
}

func TestCollectTrafficDisablesDepletedUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	nodeIDs := f.enableNodes(t)
	plan := f.newPlan(t, nodeIDs)
	u := f.newUser(t, plan.ID) // TrafficLimit 1000
	f.mgr.ReconcileUser(ctx, u)

	f.fake.SetTraffic("alice@firex", 600, 500)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}

	var got model.User
	f.db.First(&got, u.ID)
	if !got.Depleted {
		t.Fatal("Depleted = false after crossing the quota")
	}
	client := f.fake.Client("alice@firex")
	if client == nil || client.Enable {
		t.Errorf("client = %+v, want the panel to have disabled it", client)
	}
}

func TestCounterDelta(t *testing.T) {
	tests := []struct {
		name          string
		last, current int64
		want          int64
	}{
		{"first reading", 0, 50, 50},
		{"steady growth", 50, 120, 70},
		{"no change", 120, 120, 0},
		{"panel reset", 500, 30, 30},
		{"reset to zero", 500, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := counterDelta(tt.last, tt.current); got != tt.want {
				t.Errorf("counterDelta(%d, %d) = %d, want %d", tt.last, tt.current, got, tt.want)
			}
		})
	}
}

func TestDiffInts(t *testing.T) {
	missing, extra := diffInts([]int{1, 2, 4}, []int{2, 3})
	if len(missing) != 2 || missing[0] != 1 || missing[1] != 4 {
		t.Errorf("missing = %v, want [1 4]", missing)
	}
	if len(extra) != 1 || extra[0] != 3 {
		t.Errorf("extra = %v, want [3]", extra)
	}
}
