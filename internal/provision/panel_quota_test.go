package provision

import (
	"context"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/model"
)

// newSecondUser puts bob on the same plan, so panel totals can be checked
// across users.
func (f *fixture) newSecondUser(t *testing.T, planID uint) *model.User {
	t.Helper()
	u := model.User{
		Username: "bob", UUID: "uuid-bob", SubToken: "subtok-bob",
		PlanID: planID, Enabled: true, TrafficLimit: 1000,
	}
	if err := f.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return &u
}

func TestCollectTrafficSumsPanelAndPerPanelUsage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	plan := f.newPlan(t, f.enableInbounds(t))
	alice, bob := f.newUser(t, plan.ID), f.newSecondUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, alice)
	f.mgr.ReconcileUser(ctx, bob)

	f.fake.SetTraffic("alice@FireX", 10, 20)
	f.fake.SetTraffic("bob@FireX", 30, 40)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	f.fake.SetTraffic("alice@FireX", 15, 25)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}

	var p model.Panel
	f.db.First(&p, f.panel.ID)
	if p.Upload != 45 || p.Download != 65 {
		t.Errorf("panel traffic = %d/%d, want 45/65", p.Upload, p.Download)
	}
	var rec model.UserPanel
	f.db.Where("user_id = ? AND panel_id = ?", alice.ID, f.panel.ID).First(&rec)
	if rec.Upload != 15 || rec.Download != 25 {
		t.Errorf("alice's usage on the panel = %d/%d, want 15/25", rec.Upload, rec.Download)
	}
}

func TestPanelQuotaDisablesEveryClientUntilReset(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	plan := f.newPlan(t, f.enableInbounds(t))
	alice, bob := f.newUser(t, plan.ID), f.newSecondUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, alice)
	f.mgr.ReconcileUser(ctx, bob)
	f.db.Model(&model.Panel{}).Where("id = ?", f.panel.ID).Update("traffic_limit", 100)

	// Neither user is near their own 1000-byte quota; together they fill the
	// panel's 100.
	f.fake.SetTraffic("alice@FireX", 30, 30)
	f.fake.SetTraffic("bob@FireX", 20, 20)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	for _, email := range []string{"alice@FireX", "bob@FireX"} {
		if c := f.fake.Client(email); c == nil || c.Enable {
			t.Errorf("%s = %+v, want it kept but disabled", email, c)
		}
	}
	var u model.User
	f.db.First(&u, alice.ID)
	if u.Depleted {
		t.Error("alice marked depleted; only the panel ran out")
	}
	blocked, _ := f.mgr.BlockedPanels(&u)
	if blocked[f.panel.ID] != BlockedPanelQuota {
		t.Errorf("BlockedPanels() = %v, want the panel blocked for its own quota", blocked)
	}

	if err := f.mgr.ResetPanelTraffic(ctx, f.panel.ID); err != nil {
		t.Fatalf("ResetPanelTraffic() error = %v", err)
	}
	var p model.Panel
	f.db.First(&p, f.panel.ID)
	if p.Upload != 0 || p.Download != 0 || p.TrafficPeriodStart == 0 {
		t.Errorf("panel after reset = %d/%d start %d, want 0/0 and a period start", p.Upload, p.Download, p.TrafficPeriodStart)
	}
	for _, email := range []string{"alice@FireX", "bob@FireX"} {
		if c := f.fake.Client(email); c == nil || !c.Enable {
			t.Errorf("%s = %+v, want it enabled again after the reset", email, c)
		}
	}
	// The users' own usage belongs to their plans and survives a panel reset.
	f.db.First(&u, alice.ID)
	if u.Upload != 30 || u.Download != 30 {
		t.Errorf("alice's traffic = %d/%d, want 30/30 kept", u.Upload, u.Download)
	}
}

func TestPlanPanelLimitClosesOnlyThatPanelForTheUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	plan := f.newPlan(t, f.enableInbounds(t)) // 1000 bytes overall
	f.db.Create(&model.PlanPanelLimit{PlanID: plan.ID, PanelID: f.panel.ID, TrafficBytes: 100})
	alice, bob := f.newUser(t, plan.ID), f.newSecondUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, alice)
	f.mgr.ReconcileUser(ctx, bob)

	// The panel-side cap is the tighter per-panel one, so the panel itself
	// stops a user who outruns the poll.
	if c := f.fake.Client("alice@FireX"); c == nil || c.TotalGB != 100 {
		t.Fatalf("alice's client = %+v, want TotalGB 100", c)
	}

	f.fake.SetTraffic("alice@FireX", 60, 50)
	f.fake.SetTraffic("bob@FireX", 10, 10)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	if c := f.fake.Client("alice@FireX"); c == nil || c.Enable {
		t.Errorf("alice = %+v, want disabled on the panel she used up", c)
	}
	if c := f.fake.Client("bob@FireX"); c == nil || !c.Enable {
		t.Errorf("bob = %+v, want still enabled", c)
	}
	var u model.User
	f.db.First(&u, alice.ID)
	if u.Depleted {
		t.Error("alice marked depleted; she has 890 bytes of her plan left")
	}
	blocked, _ := f.mgr.BlockedPanels(&u)
	if blocked[f.panel.ID] != BlockedUserPanelQuota {
		t.Errorf("BlockedPanels() = %v, want the per-panel quota", blocked)
	}

	// Her own reset starts the per-panel usage over with the rest.
	if _, err := f.mgr.ResetTraffic(ctx, &u); err != nil {
		t.Fatalf("ResetTraffic() error = %v", err)
	}
	var rec model.UserPanel
	f.db.Where("user_id = ? AND panel_id = ?", alice.ID, f.panel.ID).First(&rec)
	if rec.Upload != 0 || rec.Download != 0 {
		t.Errorf("per-panel usage after reset = %d/%d, want 0/0", rec.Upload, rec.Download)
	}
	if c := f.fake.Client("alice@FireX"); c == nil || !c.Enable {
		t.Errorf("alice = %+v, want enabled after her reset", c)
	}
}

func TestPanelTrafficResetsOnItsDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	f.mgr.loc = loc
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, loc)
	f.mgr.now = func() time.Time { return now }

	f.db.Model(&model.Panel{}).Where("id = ?", f.panel.ID).Updates(map[string]any{
		"traffic_limit": 1000, "traffic_reset_day": 10, "upload": 400, "download": 600,
		"traffic_period_start": time.Date(2026, 2, 10, 0, 0, 0, 0, loc).UnixMilli(),
	})
	var p model.Panel
	f.db.First(&p, f.panel.ID)
	if want := time.Date(2026, 3, 10, 0, 0, 0, 0, loc).UnixMilli(); p.NextTrafficReset(loc) != want {
		t.Fatalf("NextTrafficReset() = %d, want %d", p.NextTrafficReset(loc), want)
	}

	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	f.db.First(&p, f.panel.ID)
	if p.Upload+p.Download != 1000 {
		t.Fatalf("panel reset a day early: used = %d", p.Upload+p.Download)
	}

	now = time.Date(2026, 3, 10, 0, 0, 1, 0, loc)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	f.db.First(&p, f.panel.ID)
	if p.Upload != 0 || p.Download != 0 {
		t.Errorf("panel traffic after its reset day = %d/%d, want 0/0", p.Upload, p.Download)
	}
	if want := time.Date(2026, 4, 10, 0, 0, 0, 0, loc).UnixMilli(); p.NextTrafficReset(loc) != want {
		t.Errorf("next reset = %d, want %d", p.NextTrafficReset(loc), want)
	}
}
