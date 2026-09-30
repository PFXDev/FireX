package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/paneltest"
)

func TestVisionConvergesPerInboundAcrossClientLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	ids := f.enableInbounds(t)
	if err := f.db.Model(&model.Inbound{}).Where("id = ?", ids[0]).Update("vision", true).Error; err != nil {
		t.Fatal(err)
	}
	u := f.newUser(t, f.newPlan(t, ids).ID)
	check := func(first string, count int) {
		t.Helper()
		if err := f.mgr.ReconcileUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		for id := 1; id <= count; id++ {
			want := ""
			if id == 1 {
				want = first
			}
			if got := f.fake.ClientFlow(EmailFor(u), id); got != want {
				t.Errorf("inbound %d flow = %q, want %q", id, got, want)
			}
		}
	}
	check(panel.VisionFlow, 2)
	f.fake.ResetCalls()
	check(panel.VisionFlow, 2)
	for _, call := range f.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Errorf("no-op reconcile wrote to panel: %s", call)
		}
	}
	// Panel-wide quota edits must retain the different per-inbound flows.
	u.TrafficLimit = 9000
	f.fake.SetTraffic(EmailFor(u), 100, 200)
	check(panel.VisionFlow, 2)
	if c := f.fake.Client(EmailFor(u)); c.TotalGB != 9000 || c.Up != 100 || c.Down != 200 {
		t.Fatalf("quota/traffic changed incorrectly: %+v", c)
	}
	// New attachments may inherit the canonical record's Vision flow. Clear
	// that inheritance even when the new inbound cannot support Vision.
	f.fake.AddInbound(paneltest.Inbound{ID: 3, Port: 9443, Protocol: "vless", Enable: true, StreamSettings: `{"network":"ws","security":"tls"}`})
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	allIDs := f.enableInbounds(t)
	if err := f.db.Create(&model.NodeGroupInbound{GroupID: f.group.ID, InboundID: allIDs[2]}).Error; err != nil {
		t.Fatal(err)
	}
	check(panel.VisionFlow, 3)
	f.fake.SetClientFlow(EmailFor(u), 1, "")
	f.fake.SetClientFlow(EmailFor(u), 2, panel.VisionFlow)
	check(panel.VisionFlow, 3)
	if err := f.db.Model(&model.Inbound{}).Where("id = ?", ids[0]).Update("vision", false).Error; err != nil {
		t.Fatal(err)
	}
	check("", 3)
}

func TestVisionChecksLiveTransportAndRetriesFailedSync(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	ids := f.enableInbounds(t)
	u := f.newUser(t, f.newPlan(t, ids).ID)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	f.db.Model(&model.Inbound{}).Where("id = ?", ids[0]).Update("vision", true)
	f.fake.FailNext["/clients/update/"+EmailFor(u)] = true
	if err := f.mgr.ReconcileUser(ctx, u); err == nil {
		t.Fatal("expected flow sync failure")
	}
	var state model.UserPanel
	f.db.First(&state, "user_id = ?", u.ID)
	if state.State != model.SyncStateFailed {
		t.Fatalf("sync state = %s", state.State)
	}
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if f.fake.ClientFlow(EmailFor(u), 1) != panel.VisionFlow {
		t.Fatal("retry did not apply Vision")
	}
	// Change only the remote transport, leaving discovery intentionally stale.
	f.fake.RemoveInbound(1)
	f.fake.AddInbound(paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Enable: true, StreamSettings: `{"network":"grpc","security":"reality"}`})
	f.fake.ResetCalls()
	if err := f.mgr.ReconcileUser(ctx, u); err == nil || !strings.Contains(err.Error(), "TCP / RAW") {
		t.Fatalf("live incompatible transport: %v", err)
	}
	for _, call := range f.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Errorf("incompatible transport caused write: %s", call)
		}
	}
	// Flow validation must never prevent revoking an account's access.
	u.Enabled = false
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatalf("disable account on changed transport: %v", err)
	}
	if f.fake.Client(EmailFor(u)).Enable || f.fake.ClientFlow(EmailFor(u), 1) != "" {
		t.Fatal("incompatible flow prevented account revocation")
	}
}

func TestVisionUpgradePreservesPerClientFlows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	ids := f.enableInbounds(t)
	// Schema migration leaves pre-existing inbounds unset until the admin
	// explicitly chooses a uniform flow. Users on one inbound may differ.
	if err := f.db.Model(&model.Inbound{}).Where("panel_id = ?", f.panel.ID).Update("vision", nil).Error; err != nil {
		t.Fatal(err)
	}
	u := f.newUser(t, f.newPlan(t, ids).ID)
	bob := *u
	bob.ID, bob.Username, bob.UUID, bob.SubToken = 0, "bob", "uuid-bob", "sub-bob"
	if err := f.db.Create(&bob).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []*model.User{u, &bob} {
		flow := ""
		if user.ID == u.ID {
			flow = panel.VisionFlow
		}
		if err := f.mgr.ClientFor(f.panel).AddClient(ctx, panel.RemoteClient{
			ID: user.UUID, Email: user.Username + "@firex", Enable: true, Flow: flow,
		}, []int{1, 2}); err != nil {
			t.Fatal(err)
		}
	}
	f.fake.SetClientFlow(EmailFor(u), 2, "")
	f.fake.SetTraffic(EmailFor(u), 100, 200)
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		if err := f.mgr.ReconcileUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := f.mgr.ReconcileUser(ctx, &bob); err != nil {
			t.Fatal(err)
		}
		if f.fake.ClientFlow(EmailFor(u), 1) != panel.VisionFlow || f.fake.ClientFlow(EmailFor(u), 2) != "" || f.fake.ClientFlow(EmailFor(&bob), 1) != "" {
			t.Fatal("upgrade overwrote per-client flows")
		}
	}
	check()
	f.fake.ResetCalls()
	check()
	for _, call := range f.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Fatalf("unchanged preserved flows caused a write: %s", call)
		}
	}
	u.TrafficLimit = 9000
	check()
	if c := f.fake.Client(EmailFor(u)); c.TotalGB != 9000 || c.Up != 100 || c.Down != 200 {
		t.Fatalf("quota or traffic lost while preserving flows: %+v", c)
	}
	// Preserving flow must still allow disabling and re-enabling the user.
	u.Enabled = false
	check()
	if f.fake.Client(EmailFor(u)).Enable {
		t.Fatal("preserved flow prevented disabling the user")
	}
	u.Enabled = true
	check()

	// An explicit off selection takes ownership and clears the legacy flow.
	if err := f.db.Model(&model.Inbound{}).Where("id = ?", ids[0]).Update("vision", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if f.fake.ClientFlow(EmailFor(u), 1) != "" {
		t.Fatal("explicit off failed to clear preserved Vision")
	}
}

func TestPreservedFlowDoesNotLeakIntoNewAttachments(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	ids := f.enableInbounds(t)
	if err := f.db.Model(&model.Inbound{}).Where("panel_id = ?", f.panel.ID).Update("vision", nil).Error; err != nil {
		t.Fatal(err)
	}
	u := f.newUser(t, f.newPlan(t, ids[:1]).ID)
	if err := f.mgr.ClientFor(f.panel).AddClient(ctx, panel.RemoteClient{
		ID: u.UUID, Email: EmailFor(u), Enable: true, Flow: panel.VisionFlow,
	}, []int{1}); err != nil {
		t.Fatal(err)
	}
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.NodeGroupInbound{GroupID: f.group.ID, InboundID: ids[1]}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if f.fake.ClientFlow(EmailFor(u), 1) != panel.VisionFlow || f.fake.ClientFlow(EmailFor(u), 2) != "" {
		t.Fatal("new attachment inherited an unrelated inbound's flow")
	}
}

func TestVisionErrorDoesNotBlockRevokingOtherInbounds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.mgr.DiscoverPanel(ctx, f.panel); err != nil {
		t.Fatal(err)
	}
	ids := f.enableInbounds(t)
	if err := f.db.Model(&model.Inbound{}).Where("id = ?", ids[0]).Update("vision", true).Error; err != nil {
		t.Fatal(err)
	}
	u := f.newUser(t, f.newPlan(t, ids).ID)
	if err := f.mgr.ReconcileUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("group_id = ? AND inbound_id = ?", f.group.ID, ids[1]).Delete(&model.NodeGroupInbound{}).Error; err != nil {
		t.Fatal(err)
	}
	f.fake.RemoveInbound(1)
	f.fake.AddInbound(paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Enable: true, StreamSettings: `{"network":"grpc","security":"reality"}`})
	f.fake.ResetCalls()
	if err := f.mgr.ReconcileUser(ctx, u); err == nil || !strings.Contains(err.Error(), "TCP / RAW") {
		t.Fatalf("expected remaining inbound's Vision error, got %v", err)
	}
	if got := f.fake.Members(EmailFor(u)); len(got) != 1 || got[0] != 1 {
		t.Fatalf("revoked inbound remains attached: %v", got)
	}
	for _, call := range f.fake.Calls() {
		if strings.HasPrefix(call, "POST ") && !strings.HasSuffix(call, "/detach") {
			t.Fatalf("invalid Vision must only allow revocation, got %s", call)
		}
	}
	var state model.UserPanel
	if err := f.db.First(&state, "user_id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.State != model.SyncStateFailed {
		t.Fatalf("remaining configuration error was not recorded: %s", state.State)
	}
}
