package provision

import (
	"context"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/model"
)

func TestResetUserTraffic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mgr.DiscoverPanel(ctx, f.panel)
	inboundIDs := f.enableInbounds(t)
	plan := f.newPlan(t, inboundIDs)
	u := f.newUser(t, plan.ID)
	f.mgr.ReconcileUser(ctx, u)

	f.fake.SetTraffic("alice@FireX", 400, 600)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	var got model.User
	f.db.First(&got, u.ID)
	if got.Upload != 400 || got.Download != 600 {
		t.Fatalf("traffic before reset = %d/%d, want 400/600", got.Upload, got.Download)
	}

	// Reset zeroes the user's totals and asks the panel to zero its counters.
	// The panel's new reading is taken as the whole delta, not as usage that
	// happened before the reset.
	missed, err := f.mgr.ResetTraffic(ctx, &got)
	if err != nil {
		t.Fatalf("ResetTraffic() error = %v", err)
	}
	if missed != 0 {
		t.Errorf("ResetTraffic() missed = %d, want 0", missed)
	}
	f.db.First(&got, u.ID)
	if got.Upload != 0 || got.Download != 0 {
		t.Errorf("traffic after reset = %d/%d, want 0/0", got.Upload, got.Download)
	}
	if got.TotalUpload != 400 || got.TotalDownload != 600 {
		t.Errorf("cumulative traffic after reset = %d/%d, want 400/600", got.TotalUpload, got.TotalDownload)
	}
	var upRec model.UserPanel
	f.db.First(&upRec, "user_id = ?", u.ID)
	if upRec.Upload != 0 || upRec.Download != 0 {
		t.Errorf("per-panel period traffic after reset = %d/%d, want 0/0", upRec.Upload, upRec.Download)
	}
	if upRec.TotalUpload != 400 || upRec.TotalDownload != 600 {
		t.Errorf("per-panel cumulative traffic after reset = %d/%d, want 400/600", upRec.TotalUpload, upRec.TotalDownload)
	}
	if got.TrafficPeriodStart == 0 {
		t.Error("TrafficPeriodStart = 0, want the reset timestamp")
	}
	client := f.fake.Client("alice@FireX")
	if client.Up != 0 || client.Down != 0 {
		t.Errorf("panel counter = %d/%d, want 0/0 after reset", client.Up, client.Down)
	}

	// A depleted user is re-enabled because the quota resets too.
	got.Depleted = true
	f.db.Save(&got)
	if _, err := f.mgr.ResetTraffic(ctx, &got); err != nil {
		t.Fatalf("ResetTraffic() error = %v", err)
	}
	f.db.First(&got, u.ID)
	if got.Depleted {
		t.Error("Depleted = true after reset, want false")
	}
	client = f.fake.Client("alice@FireX")
	if !client.Enable {
		t.Error("panel Enable = false after reset, want true")
	}

	// Panel failures are reported; the next poll retries the reset.
	f.fake.SetTraffic("alice@FireX", 50, 100)
	f.mgr.CollectTraffic(ctx)
	f.fake.FailNext["/clients/resetTraffic/alice@FireX"] = true
	missed, _ = f.mgr.ResetTraffic(ctx, &got)
	if missed != 1 {
		t.Errorf("missed after panel failure = %d, want 1", missed)
	}
	f.db.First(&got, u.ID)
	if got.Upload != 0 || got.Download != 0 {
		t.Errorf("traffic after failed reset = %d/%d, want 0/0 (still cleared locally)", got.Upload, got.Download)
	}
	var rec model.UserPanel
	f.db.First(&rec, "user_id = ?", u.ID)
	if !rec.ResetPending {
		t.Fatal("ResetPending = false, want true until the panel confirms")
	}
	// The next poll retries it and succeeds.
	f.fake.SetTraffic("alice@FireX", 10, 20)
	if err := f.mgr.CollectTraffic(ctx); err != nil {
		t.Fatalf("CollectTraffic() error = %v", err)
	}
	f.db.First(&rec, "user_id = ?", u.ID)
	if rec.ResetPending {
		t.Error("ResetPending = true after retry, want false")
	}
	if rec.LastUp != 0 || rec.LastDown != 0 {
		t.Errorf("panel baselines = %d/%d after retry, want 0/0", rec.LastUp, rec.LastDown)
	}
	// Usage reported after the failed reset is not charged to the old period.
	f.db.First(&got, u.ID)
	if got.Upload != 10 || got.Download != 20 {
		t.Errorf("traffic after retry = %d/%d, want 10/20", got.Upload, got.Download)
	}
	// Cumulative traffic adds across every period without double-counting the
	// failed reset.
	if got.TotalUpload != 460 || got.TotalDownload != 720 {
		t.Errorf("cumulative traffic after retry = %d/%d, want 460/720", got.TotalUpload, got.TotalDownload)
	}
	f.db.First(&rec, "user_id = ?", u.ID)
	if rec.TotalUpload != 460 || rec.TotalDownload != 720 {
		t.Errorf("per-panel cumulative traffic after retry = %d/%d, want 460/720", rec.TotalUpload, rec.TotalDownload)
	}
}

func TestResetDueTrafficAtPeriodBoundary(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("America/New_York")
	f.mgr.loc = loc
	// 2026-01-15 10:00 EST is when the test body starts.
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, loc)
	f.mgr.now = func() time.Time { return now }

	f.mgr.DiscoverPanel(ctx, f.panel)
	inboundIDs := f.enableInbounds(t)

	// Plan 1: monthly reset, user created on Jan 10 (next reset Feb 10 midnight).
	plan1 := f.newPlan(t, inboundIDs)
	plan1.TrafficReset = model.TrafficResetMonthly
	f.db.Save(plan1)
	u1 := f.newUser(t, plan1.ID)
	u1.CreatedAt = time.Date(2026, 1, 10, 14, 30, 0, 0, loc).UnixMilli()
	f.db.Save(u1)
	f.mgr.ReconcileUser(ctx, u1)

	// Plan 2: calendar reset (next reset on Feb 1 midnight).
	plan2 := model.Plan{
		Name: "cal", TrafficBytes: 999999, DurationDays: 30,
		ProfileID: plan1.ProfileID, DeviceLimit: 0, TrafficReset: model.TrafficResetCalendar,
		Enabled: true, CreatedAt: now.UnixMilli(),
	}
	f.db.Create(&plan2)
	u2 := model.User{
		Username: "bob", UUID: "uuid-bob", SubToken: "tok2",
		PlanID: plan2.ID, Enabled: true, TrafficLimit: 9999, CreatedAt: now.UnixMilli(),
	}
	f.db.Create(&u2)
	f.mgr.ReconcileUser(ctx, &u2)

	// Give each some usage.
	f.fake.SetTraffic("alice@FireX", 200, 300)
	f.fake.SetTraffic("bob@FireX", 100, 100)
	f.mgr.CollectTraffic(ctx)

	// Before the boundary: no reset.
	if err := f.mgr.resetDueTraffic(ctx); err != nil {
		t.Fatalf("resetDueTraffic() error = %v", err)
	}
	var got1, got2 model.User
	f.db.First(&got1, u1.ID)
	f.db.First(&got2, u2.ID)
	if got1.Upload+got1.Download != 500 {
		t.Errorf("u1 usage before boundary = %d, want 500", got1.Upload+got1.Download)
	}
	if got2.Upload+got2.Download != 200 {
		t.Errorf("u2 usage before boundary = %d, want 200", got2.Upload+got2.Download)
	}

	// Advance to Feb 1, one minute past midnight: u2 resets, u1 does not (waits for the 10th).
	now = time.Date(2026, 2, 1, 0, 1, 0, 0, loc)
	if err := f.mgr.resetDueTraffic(ctx); err != nil {
		t.Fatalf("resetDueTraffic() error = %v", err)
	}
	f.db.First(&got1, u1.ID)
	f.db.First(&got2, u2.ID)
	if got1.Upload+got1.Download != 500 {
		t.Errorf("u1 usage after Feb 1 = %d, want 500 (not reset yet)", got1.Upload+got1.Download)
	}
	if got2.Upload+got2.Download != 0 {
		t.Errorf("u2 usage after Feb 1 = %d, want 0 (calendar reset)", got2.Upload+got2.Download)
	}
	if got2.TrafficPeriodStart == 0 {
		t.Error("u2 TrafficPeriodStart = 0, want the reset boundary")
	}

	// Advance to Feb 10, one second past midnight: u1 resets.
	now = time.Date(2026, 2, 10, 0, 0, 1, 0, loc)
	if err := f.mgr.resetDueTraffic(ctx); err != nil {
		t.Fatalf("resetDueTraffic() error = %v", err)
	}
	f.db.First(&got1, u1.ID)
	if got1.Upload+got1.Download != 0 {
		t.Errorf("u1 usage after Feb 10 = %d, want 0 (monthly reset)", got1.Upload+got1.Download)
	}

	// FireX down all of March: multiple missed boundaries fold into one reset.
	f.fake.SetTraffic("bob@FireX", 250, 250)
	f.mgr.CollectTraffic(ctx)
	f.db.First(&got2, u2.ID)
	if got2.Upload+got2.Download != 500 {
		t.Fatalf("u2 usage in February = %d, want 500", got2.Upload+got2.Download)
	}
	now = time.Date(2026, 4, 2, 8, 0, 0, 0, loc)
	if err := f.mgr.resetDueTraffic(ctx); err != nil {
		t.Fatalf("resetDueTraffic() error = %v", err)
	}
	f.db.First(&got2, u2.ID)
	if got2.Upload+got2.Download != 0 {
		t.Errorf("u2 usage after downtime = %d, want 0 (single catch-up reset)", got2.Upload+got2.Download)
	}
	if got2.TotalUpload != 350 || got2.TotalDownload != 350 {
		t.Errorf("u2 cumulative after scheduled resets = %d/%d, want 350/350", got2.TotalUpload, got2.TotalDownload)
	}
	f.db.First(&got1, u1.ID)
	if got1.TotalUpload != 200 || got1.TotalDownload != 300 {
		t.Errorf("u1 cumulative after monthly reset = %d/%d, want 200/300", got1.TotalUpload, got1.TotalDownload)
	}
}

func TestNextTrafficResetDateMath(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	tests := []struct {
		name        string
		mode        string
		created     time.Time
		periodStart time.Time
		want        time.Time
	}{
		{
			name:        "calendar - mid-month, next is the 1st",
			mode:        model.TrafficResetCalendar,
			created:     time.Date(2026, 1, 15, 10, 0, 0, 0, loc),
			periodStart: time.Date(2026, 1, 15, 10, 0, 0, 0, loc),
			want:        time.Date(2026, 2, 1, 0, 0, 0, 0, loc),
		},
		{
			name:        "calendar - already Feb 1, next is Mar 1",
			mode:        model.TrafficResetCalendar,
			created:     time.Date(2026, 1, 10, 0, 0, 0, 0, loc),
			periodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, loc),
			want:        time.Date(2026, 3, 1, 0, 0, 0, 0, loc),
		},
		{
			name:        "monthly - created on 10th, next is 10th",
			mode:        model.TrafficResetMonthly,
			created:     time.Date(2026, 1, 10, 14, 30, 0, 0, loc),
			periodStart: time.Date(2026, 1, 10, 14, 30, 0, 0, loc),
			want:        time.Date(2026, 2, 10, 0, 0, 0, 0, loc),
		},
		{
			name:        "monthly - created on 31st, Feb clamps to 28th",
			mode:        model.TrafficResetMonthly,
			created:     time.Date(2026, 1, 31, 8, 0, 0, 0, loc),
			periodStart: time.Date(2026, 1, 31, 8, 0, 0, 0, loc),
			want:        time.Date(2026, 2, 28, 0, 0, 0, 0, loc),
		},
		{
			name:        "monthly - off-schedule reset, next boundary still on 15th",
			mode:        model.TrafficResetMonthly,
			created:     time.Date(2026, 1, 15, 10, 0, 0, 0, loc),
			periodStart: time.Date(2026, 1, 20, 9, 0, 0, 0, loc), // manual reset
			want:        time.Date(2026, 2, 15, 0, 0, 0, 0, loc),
		},
		{
			name:        "none",
			mode:        model.TrafficResetNone,
			created:     time.Date(2026, 1, 10, 0, 0, 0, 0, loc),
			periodStart: time.Date(2026, 1, 10, 0, 0, 0, 0, loc),
			want:        time.Time{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &model.User{
				CreatedAt:          tt.created.UnixMilli(),
				TrafficPeriodStart: tt.periodStart.UnixMilli(),
			}
			got := u.NextTrafficReset(tt.mode, loc)
			want := int64(0)
			if !tt.want.IsZero() {
				want = tt.want.UnixMilli()
			}
			if got != want {
				t.Errorf("NextTrafficReset() = %v, want %v", time.UnixMilli(got).In(loc), tt.want)
			}
		})
	}
}
