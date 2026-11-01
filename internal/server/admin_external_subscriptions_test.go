package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
)

func TestInactiveUsersCannotDownloadExternalNodes(t *testing.T) {
	h := newHarness(t)
	user := h.seed()
	const credential = "external-test-uuid"
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("vless://" + credential + "@example.com:443?security=tls#external"))
	}))
	defer source.Close()
	sub := model.ExternalSubscription{Name: "source", URL: source.URL, Enabled: true}
	if err := h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.srv.subs.RefreshExternalSubscription(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	var group model.NodeGroup
	if err := h.db.First(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.NodeGroupExternalSubscription{GroupID: group.ID, SubscriptionID: sub.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		enabled  bool
		depleted bool
		expires  int64
		download int64
		active   bool
	}{
		{"disabled", false, false, 0, 0, false},
		{"expired", true, false, 1, 0, false},
		{"over quota", true, false, 0, 100, false},
		{"depleted", true, true, 0, 0, false},
		{"reactivated", true, false, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := h.db.Model(user).Updates(map[string]any{
				"enabled": tc.enabled, "depleted": tc.depleted, "expires_at": tc.expires,
				"traffic_limit": 100, "download": tc.download,
			}).Error; err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"mihomo", "base64"} {
				body := h.mustDo(http.MethodGet, "/sub/"+user.SubToken+"?target="+target, nil)
				if target == "base64" {
					var err error
					body, err = base64.StdEncoding.DecodeString(string(body))
					if err != nil {
						t.Fatal(err)
					}
				}
				if strings.Contains(string(body), credential) != tc.active {
					t.Errorf("%s output: external credentials present=%v, want %v", target, strings.Contains(string(body), credential), tc.active)
				}
			}
		})
	}
}

func TestExternalSubscriptionURLChangeDuringRefresh(t *testing.T) {
	for _, oldStatus := range []int{http.StatusOK, http.StatusBadGateway} {
		t.Run(http.StatusText(oldStatus), func(t *testing.T) {
			h := newHarness(t)
			h.login()
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			releaseOld := func() { once.Do(func() { close(release) }) }
			oldSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.WriteHeader(oldStatus)
				_, _ = w.Write([]byte("vless://old-uuid@old.example.com:443#old"))
			}))
			defer oldSource.Close()
			defer releaseOld()
			newSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("vless://new-uuid@new.example.com:443#new"))
			}))
			defer newSource.Close()
			sub := model.ExternalSubscription{
				Name: "source", URL: oldSource.URL, Enabled: true, UpdateInterval: 3600,
				Nodes: "[]", LastCheckAt: time.Now().UnixMilli(),
			}
			if err := h.db.Create(&sub).Error; err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- h.srv.subs.RefreshExternalSubscription(context.Background(), sub.ID) }()
			<-started
			h.mustDo(http.MethodPut, "/api/external-subscriptions/"+itoa(sub.ID), map[string]any{
				"name": sub.Name, "url": newSource.URL,
			})
			releaseOld()
			err := <-done
			if (err == nil) != (oldStatus == http.StatusOK) {
				t.Fatalf("old refresh error = %v", err)
			}
			if err := h.db.First(&sub, sub.ID).Error; err != nil {
				t.Fatal(err)
			}
			if sub.URL != newSource.URL || sub.Nodes != "[]" || sub.LastCheckAt != 0 || sub.LastUpdateAt != 0 || sub.LastError != "" {
				t.Fatalf("old request changed new source state: %+v", sub)
			}
			if err := h.srv.subs.RefreshDueExternalSubscriptions(context.Background(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := h.db.First(&sub, sub.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sub.Nodes, "new.example.com") || sub.LastUpdateAt == 0 {
				t.Fatalf("new source was not refreshed on the next tick: %+v", sub)
			}
		})
	}
}

func TestExternalSubscriptionDetailsAfterFailedFirstRefresh(t *testing.T) {
	h := newHarness(t)
	h.login()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer source.Close()
	var created struct {
		Subscription model.ExternalSubscription `json:"subscription"`
		RefreshError string                     `json:"refreshError"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodPost, "/api/external-subscriptions", map[string]any{
		"name": "empty", "url": source.URL,
	}), &created); err != nil {
		t.Fatal(err)
	}
	if created.RefreshError == "" {
		t.Fatal("failed refresh was not reported")
	}
	var detail struct {
		Subscription model.ExternalSubscription `json:"subscription"`
		Nodes        []any                      `json:"nodes"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/external-subscriptions/"+itoa(created.Subscription.ID), nil), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Nodes == nil || len(detail.Nodes) != 0 || detail.Subscription.LastError != created.RefreshError {
		t.Fatalf("empty detail did not retain the refresh error: %+v", detail)
	}
}

func TestNodeGroupExternalSourcesAndPanelSharesCoexist(t *testing.T) {
	h := newHarness(t)
	h.login()
	panels := []model.Panel{{Name: "first", Enabled: true}, {Name: "second", Enabled: true}}
	if err := h.db.Create(&panels).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{PanelID: panels[0].ID, RemoteID: 1, Enabled: true},
		{PanelID: panels[1].ID, RemoteID: 1, Enabled: true},
	}
	if err := h.db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	sources := []model.ExternalSubscription{{Name: "first", URL: "https://example.com/first"}, {Name: "second", URL: "https://example.com/second"}}
	if err := h.db.Create(&sources).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"name": "mixed", "inboundIds": []uint{inbounds[0].ID, inbounds[1].ID},
		"subscriptionIds": []uint{sources[0].ID},
		"panelShares":     []routing.PanelShare{{PanelID: panels[0].ID, Weight: 30}, {PanelID: panels[1].ID, Weight: 70}},
	}
	var created struct {
		Group model.NodeGroup `json:"group"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodPost, "/api/node-groups", body), &created); err != nil {
		t.Fatal(err)
	}
	assertGroup := func(sourceID uint, firstWeight int) {
		t.Helper()
		var rows []nodeGroupRow
		if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/node-groups", nil), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("groups = %+v", rows)
		}
		row := rows[0]
		if len(row.InboundIDs) != 2 || len(row.SubscriptionIDs) != 1 || row.SubscriptionIDs[0] != sourceID {
			t.Fatalf("mixed membership = %+v", row)
		}
		weights := map[uint]int{}
		for _, share := range row.PanelShares {
			weights[share.PanelID] = share.Weight
		}
		if len(weights) != 2 || weights[panels[0].ID] != firstWeight || weights[panels[1].ID] != 100-firstWeight {
			t.Fatalf("panel weights = %v", weights)
		}
	}
	assertGroup(sources[0].ID, 30)
	body["subscriptionIds"] = []uint{sources[1].ID}
	body["panelShares"] = []routing.PanelShare{{PanelID: panels[0].ID, Weight: 60}, {PanelID: panels[1].ID, Weight: 40}}
	path := "/api/node-groups/" + itoa(created.Group.ID)
	h.mustDo(http.MethodPut, path, body)
	assertGroup(sources[1].ID, 60)
	delete(body, "panelShares")
	body["subscriptionIds"] = []uint{sources[0].ID}
	h.mustDo(http.MethodPut, path, body)
	assertGroup(sources[0].ID, 60)
	h.mustDo(http.MethodDelete, path, nil)
	for _, row := range []any{&model.NodeGroupInbound{}, &model.NodeGroupExternalSubscription{}, &model.NodeGroupPanelShare{}} {
		var count int64
		if err := h.db.Model(row).Where("group_id = ?", created.Group.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("deleting group left %d %T rows", count, row)
		}
	}
}

func TestExternalSubscriptionAPIGroupAndSnapshot(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: external-hk
    type: hysteria2
    server: hk.example.com
    port: 443
    password: secret
`))
	}))
	defer source.Close()

	h := newHarness(t)
	h.login()
	// The row embeds the model for its state fields and carries URL/count at the
	// same level, so decode into a flatter view for assertions.
	var rawCreated struct {
		ID           uint   `json:"id"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		NodeCount    int    `json:"nodeCount"`
		RefreshError string `json:"refreshError"`
	}
	var response struct {
		Subscription json.RawMessage `json:"subscription"`
		RefreshError string          `json:"refreshError"`
	}
	raw := h.mustDo(http.MethodPost, "/api/external-subscriptions", map[string]any{
		"name": "airport", "url": source.URL + "/sub?token=secret-token", "enabled": true, "updateInterval": 3600,
	})
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.Subscription, &rawCreated); err != nil {
		t.Fatal(err)
	}
	rawCreated.RefreshError = response.RefreshError
	if rawCreated.ID == 0 || rawCreated.NodeCount != 1 || rawCreated.RefreshError != "" || rawCreated.URL == redactedValue {
		t.Fatalf("created subscription = %+v", rawCreated)
	}
	raw = h.mustDo(http.MethodPost, "/api/node-groups", map[string]any{
		"name": "external group", "type": "url-test", "inboundIds": []uint{}, "subscriptionIds": []uint{rawCreated.ID},
	})
	var groupResponse struct {
		Group model.NodeGroup `json:"group"`
	}
	if err := json.Unmarshal(raw, &groupResponse); err != nil {
		t.Fatal(err)
	}

	var groups []struct {
		ID                  uint   `json:"id"`
		SubscriptionIDs     []uint `json:"subscriptionIds"`
		UsableExternalNodes int    `json:"usableExternalNodes"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/node-groups", nil), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].SubscriptionIDs) != 1 || groups[0].SubscriptionIDs[0] != rawCreated.ID || groups[0].UsableExternalNodes != 1 {
		t.Fatalf("node groups = %+v", groups)
	}

	raw = h.mustDo(http.MethodGet, "/api/external-subscriptions/"+itoa(rawCreated.ID), nil)
	var detail struct {
		Nodes []struct {
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
			Server   string `json:"server"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Nodes) != 1 || detail.Nodes[0].Protocol != "hysteria2" || detail.Nodes[0].Server != "hk.example.com" {
		t.Fatalf("node detail = %+v", detail)
	}
	if strings.Contains(string(raw), `"password"`) || strings.Contains(string(raw), `"nodes":"`) {
		t.Error("node detail exposed cached proxy credentials")
	}

	doc, err := exportSnapshot(h.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.ExternalSubscriptions) != 1 || doc.ExternalSubscriptions[0].URL != "" {
		t.Fatalf("snapshot external subscriptions = %+v", doc.ExternalSubscriptions)
	}
	if len(doc.NodeGroups) != 1 || len(doc.NodeGroups[0].ExternalSubscriptions) != 1 || doc.NodeGroups[0].ExternalSubscriptions[0] != "airport" {
		t.Fatalf("snapshot group references = %+v", doc.NodeGroups)
	}
	encoded, _ := json.Marshal(doc)
	if strings.Contains(string(encoded), "secret-token") || strings.Contains(string(encoded), "password") {
		t.Error("snapshot exposed a subscription URL or cached credentials")
	}
	if err := applySnapshotExternalSubscriptions(h.db.DB, doc.ExternalSubscriptions); err != nil {
		t.Fatalf("reapply external subscriptions: %v", err)
	}
	if err := applySnapshotNodeGroups(h.db.DB, doc.NodeGroups); err != nil {
		t.Fatalf("reapply node groups: %v", err)
	}
	var stored model.ExternalSubscription
	if err := h.db.First(&stored, rawCreated.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.URL != source.URL+"/sub?token=secret-token" || stored.Nodes == "" {
		t.Error("snapshot round trip changed the hidden URL or cache")
	}

	staff := makeOperator(t, h, "viewer")
	staff.CanViewConfig = true
	if err := h.db.Save(staff).Error; err != nil {
		t.Fatal(err)
	}
	h.loginAs("viewer", "password123")
	var viewerRows []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/external-subscriptions", nil), &viewerRows); err != nil {
		t.Fatal(err)
	}
	if len(viewerRows) != 1 || viewerRows[0].URL != redactedValue {
		t.Fatalf("read-only subscription URLs = %+v", viewerRows)
	}
	if resp, raw := h.do(http.MethodPost, "/api/external-subscriptions/"+itoa(rawCreated.ID)+"/refresh", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only refresh = %d: %s", resp.StatusCode, raw)
	}

	h.login()
	h.mustDo(http.MethodDelete, "/api/external-subscriptions/"+itoa(rawCreated.ID), nil)
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/node-groups", nil), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].SubscriptionIDs) != 0 {
		t.Errorf("deleting a source left a group reference: %+v", groups)
	}
}
