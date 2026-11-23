package server

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/store"
)

func TestOverviewEnrichedStatsAndAttention(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Create 3 panels: p1 online & near quota, p2 online & over quota, p3 disabled & offline.
	fake2 := paneltest.New("tok2", paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "sg", Enable: true})
	t.Cleanup(fake2.Close)
	fake3 := paneltest.New("tok3", paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "us", Enable: true})
	fake3URL := fake3.URL()
	fake3.Close()

	var created1 struct{ Panel model.Panel }
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p1-hk", "baseUrl": h.fake.URL(), "apiToken": "tok", "trafficLimit": 1000,
	}), &created1)
	var created2 struct{ Panel model.Panel }
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p2-sg", "baseUrl": fake2.URL(), "apiToken": "tok2", "trafficLimit": 500,
	}), &created2)
	var created3 struct{ Panel model.Panel }
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p3-disabled", "baseUrl": fake3URL, "apiToken": "tok3", "enabled": false,
	}), &created3)

	// Mark p1 near quota (850/1000), p2 over quota (600/500), p3 offline & disabled.
	h.db.Model(&model.Panel{}).Where("id = ?", created1.Panel.ID).Updates(map[string]any{
		"upload": 250, "download": 600, "total_upload": 250, "total_download": 600,
	})
	h.db.Model(&model.Panel{}).Where("id = ?", created2.Panel.ID).Updates(map[string]any{
		"upload": 200, "download": 400, "total_upload": 200, "total_download": 400,
	})
	h.db.Model(&model.Panel{}).Where("id = ?", created3.Panel.ID).Updates(map[string]any{
		"enabled": false, "status": model.PanelStatusOffline,
	})

	// Enable one inbound on p1 without adding it to any group (ungrouped), and
	// attach a pending managed config to it.
	var ib1 model.Inbound
	if err := h.db.Where("panel_id = ?", created1.Panel.ID).Order("id ASC").First(&ib1).Error; err != nil {
		t.Fatalf("load inbound: %v", err)
	}
	h.db.Model(&ib1).Update("enabled", true)
	h.db.Create(&model.InboundConfig{
		InboundID:       ib1.ID,
		Config:          ib1.Config,
		Revision:        2,
		Applied:         ib1.Config,
		AppliedRevision: 1,
	})

	// Create a plan and users covering active, expiringSoon, nearDepletion, expired, depleted, disabled.
	var prof model.Profile
	h.db.First(&prof)
	var plan model.Plan
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/plans", map[string]any{
		"name": "Pro", "profileId": prof.ID, "trafficBytes": 1000,
	}), &plan)

	now := time.Now().UnixMilli()
	dayMs := int64(24 * time.Hour / time.Millisecond)
	seedUsers := []model.User{
		{
			Username: "alice", UUID: "u1", SubToken: "t1", PlanID: plan.ID, Enabled: true,
			TrafficLimit: 1000, Upload: 300, Download: 600, TotalUpload: 300, TotalDownload: 600, // active + nearDepletion + expiringSoon
			ExpiresAt: now + 3*dayMs, LastSubAt: now - int64(time.Hour/time.Millisecond), LastSubUA: "FlClash/v0.8.90 clash-verge Platform/macos",
		},
		{
			Username: "bob", UUID: "u2", SubToken: "t2", PlanID: plan.ID, Enabled: true,
			TrafficLimit: 1000, Upload: 100, Download: 100, TotalUpload: 500, TotalDownload: 500,
			ExpiresAt: now - dayMs, LastSubAt: now - 3*dayMs, LastSubUA: "Shadowrocket/2100",
		},
		{
			Username: "carol", UUID: "u3", SubToken: "t3", PlanID: plan.ID, Enabled: true, Depleted: true,
			TrafficLimit: 1000, Upload: 400, Download: 600, TotalUpload: 400, TotalDownload: 600,
			LastSubAt: now - 10*dayMs, LastSubUA: "clash.meta/1.18",
		},
		{
			Username: "dave", UUID: "u4", SubToken: "t4", PlanID: plan.ID, Enabled: false,
			Upload: 10, Download: 10, TotalUpload: 10, TotalDownload: 10,
		},
	}
	for i := range seedUsers {
		if err := h.db.Create(&seedUsers[i]).Error; err != nil {
			t.Fatalf("create user %s: %v", seedUsers[i].Username, err)
		}
	}
	// GORM applies the `default:true` tag to zero-value booleans on Create.
	if err := h.db.Model(&seedUsers[3]).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable dave: %v", err)
	}

	// Failed external subscription, routing review flag, and submitted agent session.
	h.db.Create(&model.ExternalSubscription{
		Name: "ext-broken", URL: "https://example.com/sub", Enabled: true, LastError: "dial timeout",
	})
	_ = h.db.SetSetting(store.SettingRoutingReview, "1")
	h.db.Create(&model.AgentSession{
		AdminID: 1, TokenHash: "hash1", Task: "edit", State: model.AgentSubmitted,
		ExpiresAt: now + dayMs, CreatedAt: now,
	})

	var ov struct {
		Counts struct {
			Panels                      int64 `json:"panels"`
			EnabledPanels               int64 `json:"enabledPanels"`
			DisabledPanels              int64 `json:"disabledPanels"`
			OnlinePanels                int64 `json:"onlinePanels"`
			OfflinePanels               int64 `json:"offlinePanels"`
			OverQuotaPanels             int64 `json:"overQuotaPanels"`
			NearQuotaPanels             int64 `json:"nearQuotaPanels"`
			PendingInbounds             int64 `json:"pendingInbounds"`
			UngroupedInbounds           int64 `json:"ungroupedInbounds"`
			ExternalSubscriptions       int64 `json:"externalSubscriptions"`
			FailedExternalSubscriptions int64 `json:"failedExternalSubscriptions"`
		} `json:"counts"`
		PanelPeriodUpload   int64 `json:"panelPeriodUpload"`
		PanelPeriodDownload int64 `json:"panelPeriodDownload"`
		UserBreakdown       struct {
			Active        int64 `json:"active"`
			Disabled      int64 `json:"disabled"`
			Expired       int64 `json:"expired"`
			Depleted      int64 `json:"depleted"`
			ExpiringSoon  int64 `json:"expiringSoon"`
			NearDepletion int64 `json:"nearDepletion"`
			SubActive24h  int64 `json:"subActive24h"`
			SubActive7d   int64 `json:"subActive7d"`
			SubFetched    int64 `json:"subFetched"`
		} `json:"userBreakdown"`
		Clients []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"clients"`
		TrafficByPlan []struct {
			Name     string `json:"name"`
			Upload   int64  `json:"upload"`
			Download int64  `json:"download"`
		} `json:"trafficByPlan"`
		TopUsers struct {
			Period []struct {
				Username string `json:"username"`
			} `json:"period"`
			Total []struct {
				Username string `json:"username"`
			} `json:"total"`
		} `json:"topUsers"`
		Attention struct {
			RoutingNeedsReview   bool  `json:"routingNeedsReview"`
			PendingAgentSessions int64 `json:"pendingAgentSessions"`
		} `json:"attention"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/overview", nil), &ov)

	if ov.Counts.Panels != 3 || ov.Counts.EnabledPanels != 2 || ov.Counts.DisabledPanels != 1 {
		t.Fatalf("panel enabled/disabled counts = %+v, want 3/2/1", ov.Counts)
	}
	if ov.Counts.OnlinePanels != 2 || ov.Counts.OfflinePanels != 0 {
		t.Fatalf("disabled offline panel must not count as offline: %+v", ov.Counts)
	}
	if ov.Counts.OverQuotaPanels != 1 || ov.Counts.NearQuotaPanels != 1 {
		t.Fatalf("quota counts = %+v, want 1 over / 1 near", ov.Counts)
	}
	if ov.Counts.PendingInbounds != 1 || ov.Counts.UngroupedInbounds != 1 {
		t.Fatalf("inbound attention counts = %+v, want 1 pending / 1 ungrouped", ov.Counts)
	}
	if ov.Counts.ExternalSubscriptions != 1 || ov.Counts.FailedExternalSubscriptions != 1 {
		t.Fatalf("external sub counts = %+v, want 1/1", ov.Counts)
	}
	if ov.PanelPeriodUpload != 450 || ov.PanelPeriodDownload != 1000 {
		t.Fatalf("panel period traffic = %d/%d, want 450/1000", ov.PanelPeriodUpload, ov.PanelPeriodDownload)
	}
	if ov.UserBreakdown.Active != 1 || ov.UserBreakdown.Expired != 1 || ov.UserBreakdown.Depleted != 1 || ov.UserBreakdown.Disabled != 1 {
		t.Fatalf("user breakdown = %+v, want 1/1/1/1", ov.UserBreakdown)
	}
	if ov.UserBreakdown.ExpiringSoon != 1 || ov.UserBreakdown.NearDepletion != 1 {
		t.Fatalf("user warnings = %+v, want 1 expiring / 1 near depletion", ov.UserBreakdown)
	}
	if ov.UserBreakdown.SubActive24h != 1 || ov.UserBreakdown.SubActive7d != 2 || ov.UserBreakdown.SubFetched != 3 {
		t.Fatalf("sub activity = %+v, want 1/2/3", ov.UserBreakdown)
	}
	if len(ov.Clients) != 3 {
		t.Fatalf("clients = %+v, want 3 distinct client families", ov.Clients)
	}
	if len(ov.TrafficByPlan) == 0 || ov.TrafficByPlan[0].Name != "Pro" || ov.TrafficByPlan[0].Upload != 810 {
		t.Fatalf("trafficByPlan = %+v", ov.TrafficByPlan)
	}
	if len(ov.TopUsers.Period) < 2 || ov.TopUsers.Period[0].Username != "carol" || ov.TopUsers.Period[1].Username != "alice" {
		t.Fatalf("topUsers.period = %+v, want carol then alice", ov.TopUsers.Period)
	}
	if !ov.Attention.RoutingNeedsReview || ov.Attention.PendingAgentSessions != 1 {
		t.Fatalf("attention = %+v, want routing review + 1 pending agent", ov.Attention)
	}
}

func TestOverviewTrafficHistoryAndTodayComparison(t *testing.T) {
	h := newHarness(t)
	h.login()

	var created struct{ Panel model.Panel }
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p1", "baseUrl": h.fake.URL(), "apiToken": "tok",
	}), &created)
	panelID := created.Panel.ID

	loc := h.srv.mgr.Loc()
	now := h.srv.mgr.Now().In(loc)
	todayHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
	yesterdaySameHour := todayHour.AddDate(0, 0, -1)
	yesterdayLaterHour := time.Date(yesterdaySameHour.Year(), yesterdaySameHour.Month(), yesterdaySameHour.Day(), 23, 0, 0, 0, loc)
	twoDaysAgo := todayHour.AddDate(0, 0, -2)

	buckets := []model.TrafficBucket{
		{PanelID: panelID, BucketTs: todayHour.UnixMilli(), Upload: 120, Download: 380, UpdatedAt: now.UnixMilli()},
		{PanelID: panelID, BucketTs: yesterdaySameHour.UnixMilli(), Upload: 50, Download: 150, UpdatedAt: now.UnixMilli()},
		{PanelID: panelID, BucketTs: twoDaysAgo.UnixMilli(), Upload: 100, Download: 300, UpdatedAt: now.UnixMilli()},
	}
	if yesterdayLaterHour.After(yesterdaySameHour) {
		buckets = append(buckets, model.TrafficBucket{
			PanelID: panelID, BucketTs: yesterdayLaterHour.UnixMilli(), Upload: 30, Download: 70, UpdatedAt: now.UnixMilli(),
		})
	}
	for _, b := range buckets {
		if err := h.db.Create(&b).Error; err != nil {
			t.Fatalf("seed bucket: %v", err)
		}
	}

	var ov struct {
		TodayTraffic struct {
			TodayUpload               int64 `json:"todayUpload"`
			TodayDownload             int64 `json:"todayDownload"`
			YesterdayUpload           int64 `json:"yesterdayUpload"`
			YesterdayDownload         int64 `json:"yesterdayDownload"`
			YesterdaySameTimeUpload   int64 `json:"yesterdaySameTimeUpload"`
			YesterdaySameTimeDownload int64 `json:"yesterdaySameTimeDownload"`
		} `json:"todayTraffic"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/overview", nil), &ov)
	if ov.TodayTraffic.TodayUpload != 120 || ov.TodayTraffic.TodayDownload != 380 {
		t.Fatalf("todayTraffic today = %+v, want 120/380", ov.TodayTraffic)
	}
	if ov.TodayTraffic.YesterdaySameTimeUpload != 50 || ov.TodayTraffic.YesterdaySameTimeDownload != 150 {
		t.Fatalf("todayTraffic yesterday same time = %+v, want 50/150", ov.TodayTraffic)
	}

	type historyResp struct {
		Range         string `json:"range"`
		Granularity   string `json:"granularity"`
		TotalUpload   int64  `json:"totalUpload"`
		TotalDownload int64  `json:"totalDownload"`
		PeakTotal     int64  `json:"peakTotal"`
		Buckets       []struct {
			Ts       int64 `json:"ts"`
			Upload   int64 `json:"upload"`
			Download int64 `json:"download"`
		} `json:"buckets"`
	}
	var h24 historyResp
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/overview/traffic-history?range=24h&panelId="+strconv.FormatUint(uint64(panelID), 10), nil), &h24)
	if h24.Granularity != "hour" || len(h24.Buckets) != 24 {
		t.Fatalf("24h history = %s (%d buckets), want hour (24)", h24.Granularity, len(h24.Buckets))
	}
	if h24.Buckets[len(h24.Buckets)-1].Upload != 120 || h24.Buckets[len(h24.Buckets)-1].Download != 380 {
		t.Fatalf("latest 24h bucket = %+v, want 120/380", h24.Buckets[len(h24.Buckets)-1])
	}

	var h7 historyResp
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/overview/traffic-history?range=7d", nil), &h7)
	if h7.Granularity != "day" || len(h7.Buckets) != 7 {
		t.Fatalf("7d history = %s (%d buckets), want day (7)", h7.Granularity, len(h7.Buckets))
	}
	if h7.PeakTotal < 500 {
		t.Fatalf("7d peakTotal = %d, want >= 500", h7.PeakTotal)
	}

	resp, _ := h.do(http.MethodGet, "/api/overview/traffic-history?range=99d", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid range status = %d, want 400", resp.StatusCode)
	}
}

func TestLiveHubUpgradesActivePanelsXrayDiskOnlineUsersAndHistoryRetention(t *testing.T) {
	h := newHarness(t)
	configureFastLiveHub(h)

	h.fake.SetStatus(map[string]any{
		"cpu": 30.0, "cpuCores": 2, "logicalPro": 4,
		"mem":      map[string]any{"current": uint64(2000), "total": uint64(4000)},
		"disk":     map[string]any{"current": uint64(9000), "total": uint64(10000)},
		"loads":    []float64{2.0, 1.5, 1.0},
		"tcpCount": 25, "udpCount": 5,
		"netIO": map[string]any{"up": uint64(0), "down": uint64(0)},
		"xray":  map[string]any{"state": "error", "errorMsg": "port in use", "version": "25.1.1"},
	})
	h.fake.SetOnlineClients([]string{})

	h.login()
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "node-idle", "baseUrl": h.fake.URL(), "apiToken": "tok",
	})

	ctx1, cancel1 := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx1, http.MethodGet, h.server.URL+"/api/overview/live?interval=5", nil)
	resp1, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	r1 := bufio.NewReader(resp1.Body)
	snap := readNextSnapshot(t, r1, 2*time.Second)
	cancel1()
	resp1.Body.Close()

	// Mere TCP sockets with zero netIO and zero clients must not mark panel active.
	if snap.Summary.ActivePanels != 0 {
		t.Fatalf("activePanels = %d, want 0 when idle with only TCP sockets", snap.Summary.ActivePanels)
	}
	if snap.Summary.XrayAbnormalPanels != 1 || snap.Summary.HighDiskPanels != 1 {
		t.Fatalf("xray/disk alerts = %d/%d, want 1/1", snap.Summary.XrayAbnormalPanels, snap.Summary.HighDiskPanels)
	}
	if math.Abs(snap.Summary.FleetMemPercent-50.0) > 0.1 || math.Abs(snap.Summary.FleetLoadPerCore-0.5) > 0.01 {
		t.Fatalf("fleet mem/loadPerCore = %v/%v, want 50/0.5", snap.Summary.FleetMemPercent, snap.Summary.FleetLoadPerCore)
	}
	if snap.IntervalMs != 5000 {
		t.Fatalf("intervalMs = %d, want 5000", snap.IntervalMs)
	}
	if snap.Panels[0].DiskPercent != 90 || snap.Panels[0].XrayError != "port in use" {
		t.Fatalf("panel disk/xrayError = %v/%q, want 90/'port in use'", snap.Panels[0].DiskPercent, snap.Panels[0].XrayError)
	}

	// Wait past idleGrace so collector stops, then reconnect within historyRetention:
	// the new subscriber should receive the preserved history on its first frame.
	time.Sleep(60 * time.Millisecond)
	h.fake.SetOnlineClients([]string{"Alice@FireX"})

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	resp2 := openSSE(t, h, ctx2)
	defer resp2.Body.Close()
	snap2 := readNextSnapshot(t, bufio.NewReader(resp2.Body), 2*time.Second)

	if len(snap2.History) < 2 {
		t.Fatalf("reconnected history len = %d, want >= 2 preserved points", len(snap2.History))
	}
	if len(snap2.OnlineUsers) != 1 || snap2.OnlineUsers[0].Username != "Alice" || len(snap2.OnlineUsers[0].Panels) != 1 {
		t.Fatalf("onlineUsers = %+v, want Alice on node-idle", snap2.OnlineUsers)
	}
}
