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
	group *model.NodeGroup
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

// enableInbounds turns on every discovered inbound and returns their FireX ids
// in remote-id order.
func (f *fixture) enableInbounds(t *testing.T) []uint {
	t.Helper()
	var inbounds []model.Inbound
	if err := f.db.Order("remote_id ASC").Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	ids := make([]uint, 0, len(inbounds))
	for i := range inbounds {
		inbounds[i].Enabled = true
		if err := f.db.Save(&inbounds[i]).Error; err != nil {
			t.Fatalf("enable inbound: %v", err)
		}
		ids = append(ids, inbounds[i].ID)
	}
	return ids
}

// newPlan builds the whole chain a user needs to reach these inbounds: one node
// group holding them, one profile whitelisting that group, and a plan bound to
// the profile. The group is kept on the fixture so a test can widen or narrow it.
func (f *fixture) newPlan(t *testing.T, inboundIDs []uint) *model.Plan {
	t.Helper()
	group := model.NodeGroup{Name: "g1", Type: model.GroupTypeURLTest, Multiplier: 1, Enabled: true}
	if err := f.db.Create(&group).Error; err != nil {
		t.Fatalf("create node group: %v", err)
	}
	f.group = &group
	for _, id := range inboundIDs {
		if err := f.db.Create(&model.NodeGroupInbound{GroupID: group.ID, InboundID: id}).Error; err != nil {
			t.Fatalf("link group inbound: %v", err)
		}
	}
	profile := model.Profile{Name: "basic", Enabled: true}
	if err := f.db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if err := f.db.Create(&model.ProfileNodeGroup{ProfileID: profile.ID, GroupID: group.ID}).Error; err != nil {
		t.Fatalf("whitelist group: %v", err)
	}
	plan := model.Plan{Name: "basic", ProfileID: profile.ID, TrafficBytes: 1000, DeviceLimit: 3, Enabled: true}
	if err := f.db.Create(&plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
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

func TestDiscoverCreatesDisabledInbounds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var inbounds []model.Inbound
	f.db.Order("remote_id ASC").Find(&inbounds)
	if len(inbounds) != 2 {
		t.Fatalf("len(inbounds) = %d, want 2", len(inbounds))
	}
	// A newly discovered inbound must not silently reach every subscription.
	for _, n := range inbounds {
		if n.Enabled {
			t.Errorf("inbound %d discovered already enabled", n.RemoteID)
		}
	}
	if inbounds[0].Port != 443 || inbounds[0].Protocol != "vless" || inbounds[0].RemoteRemark != "hk-reality" {
		t.Errorf("inbound 1 = %+v", inbounds[0])
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

	var inbound model.Inbound
	f.db.First(&inbound, "remote_id = ?", 2)
	inbound.Enabled = true
	inbound.Name = "Tokyo 01"
	inbound.Emoji = "🇯🇵"
	f.db.Save(&inbound)

	f.fake.RemoveInbound(2)
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var after model.Inbound
	f.db.First(&after, inbound.ID)
	if !after.Missing {
		t.Error("Missing = false after the inbound disappeared upstream")
	}
	// Deleting the row would lose the group membership an admin curated.
	if after.Name != "Tokyo 01" || after.Emoji != "🇯🇵" {
		t.Errorf("admin fields lost: %+v", after)
	}

	f.fake.AddInbound(paneltest.Inbound{ID: 2, Port: 8443, Protocol: "vless", Remark: "jp-reality", Enable: true})
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}
	f.db.First(&after, inbound.ID)
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

func TestReconcileCreatesClientOnProfileInbounds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}
	inboundIDs := f.enableInbounds(t)
	// Only the first inbound is in the profile's node group.
	plan := f.newPlan(t, inboundIDs[:1])
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

func TestReconcileFollowsNodeGroupMembership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs[:1])
	u := f.newUser(t, plan.ID)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}

	// Widen the group to both inbounds.
	f.db.Create(&model.NodeGroupInbound{GroupID: f.group.ID, InboundID: inboundIDs[1]})
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 2 {
		t.Fatalf("members = %v, want both inbounds", got)
	}

	// Narrow it back to the second inbound only.
	f.db.Where("group_id = ? AND inbound_id = ?", f.group.ID, inboundIDs[0]).Delete(&model.NodeGroupInbound{})
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 1 || got[0] != 2 {
		t.Errorf("members = %v, want [2]", got)
	}
}

// A profile that takes every group must pick up a group created after it.
func TestReconcileFollowsAllGroupsProfile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs[:1])
	u := f.newUser(t, plan.ID)
	f.db.Model(&model.Profile{}).Where("id = ?", plan.ProfileID).Update("all_groups", true)

	extra := model.NodeGroup{Name: "later", Type: model.GroupTypeURLTest, Enabled: true}
	f.db.Create(&extra)
	f.db.Create(&model.NodeGroupInbound{GroupID: extra.ID, InboundID: inboundIDs[1]})

	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	if got := f.fake.Members("alice@firex"); len(got) != 2 {
		t.Errorf("members = %v, want the new group's inbound included", got)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
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
