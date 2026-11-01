package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
)

const testShareLink = "vless://uuid-external@example.com:443?security=tls&sni=example.com&type=ws&host=cdn.example.com&path=%2Fws#shared"

const testClashSubscription = `proxies:
  - name: shared
    type: hysteria2
    server: hy2.example.com
    port: 8443
    password: secret
    sni: hy2.example.com
    skip-cert-verify: true
`

func TestParseExternalSubscriptionFormats(t *testing.T) {
	plain, warning, err := parseExternalSubscription([]byte(testShareLink + "\nnot-a-link\n"))
	if err != nil {
		t.Fatalf("parse plain subscription: %v", err)
	}
	if len(plain) != 1 || plain[0].Link != testShareLink || plain[0].Protocol != "vless" {
		t.Fatalf("plain nodes = %+v", plain)
	}
	if warning == "" {
		t.Error("partial parse warning is empty")
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(testShareLink + "\n"))
	fromBase64, _, err := parseExternalSubscription([]byte(encoded))
	if err != nil || len(fromBase64) != 1 {
		t.Fatalf("parse base64 subscription: nodes=%d err=%v", len(fromBase64), err)
	}

	fromYAML, warning, err := parseExternalSubscription([]byte(testClashSubscription))
	if err != nil {
		t.Fatalf("parse mihomo subscription: %v", err)
	}
	if warning != "" || len(fromYAML) != 1 || fromYAML[0].Link != "" {
		t.Fatalf("yaml nodes=%+v warning=%q", fromYAML, warning)
	}
	var proxy struct {
		Name            string `yaml:"name"`
		Type            string `yaml:"type"`
		SkipCertificate bool   `yaml:"skip-cert-verify"`
	}
	if err := yaml.Unmarshal([]byte(fromYAML[0].Config), &proxy); err != nil {
		t.Fatal(err)
	}
	if proxy.Name != "shared" || proxy.Type != "hysteria2" || !proxy.SkipCertificate {
		t.Errorf("cached mihomo options were not preserved: %+v", proxy)
	}
}

func newExternalTestService(t *testing.T) (*store.DB, *Service) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, NewService(db, provision.NewManager(db))
}

func TestRefreshExternalSubscriptionKeepsLastGoodNodes(t *testing.T) {
	var responseMode atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch responseMode.Load() {
		case 1:
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		case 2:
			_, _ = w.Write([]byte("proxies:\n  - name: broken\n    type: vless\n"))
			return
		}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(testShareLink))))
	}))
	defer source.Close()

	db, svc := newExternalTestService(t)
	sub := model.ExternalSubscription{
		Name: "airport", URL: source.URL + "/sub?token=secret-token", Enabled: true, UpdateInterval: 60,
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshExternalSubscription(context.Background(), sub.ID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	oldNodes, oldSuccess := sub.Nodes, sub.LastUpdateAt
	if ExternalNodeCount(&sub) != 1 || oldSuccess == 0 {
		t.Fatalf("successful refresh state = %+v", sub)
	}

	for _, mode := range []int32{1, 2} {
		responseMode.Store(mode)
		if err := svc.RefreshExternalSubscription(context.Background(), sub.ID); err == nil {
			t.Fatalf("response mode %d: invalid refresh returned nil", mode)
		}
		if err := db.First(&sub, sub.ID).Error; err != nil {
			t.Fatal(err)
		}
		if sub.Nodes != oldNodes || sub.LastUpdateAt != oldSuccess || ExternalNodeCount(&sub) != 1 {
			t.Error("failed refresh replaced the last good cache")
		}
		if sub.LastError == "" || strings.Contains(sub.LastError, "secret-token") {
			t.Errorf("unsafe or missing refresh error: %q", sub.LastError)
		}
	}
}

func TestRefreshDueExternalSubscriptionsUsesPerSourceSchedule(t *testing.T) {
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(testShareLink))
	}))
	defer source.Close()

	db, svc := newExternalTestService(t)
	now := time.Now()
	sub := model.ExternalSubscription{
		Name: "scheduled", URL: source.URL, Enabled: true, UpdateInterval: 60, LastCheckAt: now.UnixMilli(),
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshDueExternalSubscriptions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("subscription refreshed before its interval elapsed")
	}
	if err := db.Model(&sub).Update("last_check_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshDueExternalSubscriptions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("due refresh calls = %d, want 1", calls.Load())
	}
	if err := svc.RefreshDueExternalSubscriptions(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("second immediate run repeated the refresh")
	}
	if err := db.Model(&sub).Updates(map[string]any{"enabled": false, "last_check_at": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshDueExternalSubscriptions(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("disabled subscription was refreshed")
	}
}

func createRefreshedSubscription(t *testing.T, db *store.DB, svc *Service, name, rawURL string) model.ExternalSubscription {
	t.Helper()
	sub := model.ExternalSubscription{Name: name, URL: rawURL, Enabled: true, UpdateInterval: 3600}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshExternalSubscription(context.Background(), sub.ID); err != nil {
		t.Fatalf("refresh %s: %v", name, err)
	}
	return sub
}

func TestMixedSubscriptionKeepsExternalNodesWhenPanelQuotaIsExhausted(t *testing.T) {
	f := newFixture(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testClashSubscription))
	}))
	defer source.Close()
	sub := createRefreshedSubscription(t, f.db, f.svc, "external", source.URL)
	if err := f.db.Create(&model.NodeGroupExternalSubscription{GroupID: f.groups[0].ID, SubscriptionID: sub.ID}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("mixed subscription entries = %d, want 3", len(result.Entries))
	}
	var panel model.Panel
	if err := f.db.First(&panel).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&panel).Updates(map[string]any{"traffic_limit": 100, "download": 100}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ExternalSubscriptionID != sub.ID {
		t.Fatalf("quota filtering should retain only external nodes: %+v", result.Entries)
	}
	out, err := f.svc.Clash(result, "https://sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	members := renderedGroups(t, out)[f.groups[0].DisplayName()]
	if len(members) != 1 || members[0] != "shared" {
		t.Fatalf("mixed group after panel quota exhaustion = %v", members)
	}
}

func TestExternalOnlyUserSubscriptionWithMultipleSources(t *testing.T) {
	f := newFixture(t)
	shareSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(testShareLink))))
	}))
	defer shareSource.Close()
	yamlSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testClashSubscription))
	}))
	defer yamlSource.Close()

	first := createRefreshedSubscription(t, f.db, f.svc, "share source", shareSource.URL+"/secret-path")
	second := createRefreshedSubscription(t, f.db, f.svc, "yaml source", yamlSource.URL)
	group := model.NodeGroup{Name: "外部线路", Type: model.GroupTypeURLTest, Enabled: true, SortOrder: 1}
	profile := model.Profile{Name: "external only", Enabled: true}
	if err := f.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	for _, sub := range []model.ExternalSubscription{first, second} {
		if err := f.db.Create(&model.NodeGroupExternalSubscription{GroupID: group.ID, SubscriptionID: sub.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Create(&model.ProfileNodeGroup{ProfileID: profile.ID, GroupID: group.ID}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "external plan", ProfileID: profile.ID, Enabled: true}
	if err := f.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "external", UUID: "uuid-external-user", SubToken: "token-external", PlanID: plan.ID, Enabled: true}
	if err := f.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	result, err := f.svc.Build(context.Background(), &user)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("external-only entries = %d, want 2 (warnings %v)", len(result.Entries), result.Warnings)
	}
	if result.Entries[0].Inbound.ID != 0 || result.Entries[0].ExternalSubscriptionID == 0 {
		t.Fatalf("unexpected external source identity: %+v", result.Entries[0])
	}
	if result.Entries[0].Name != "shared" || result.Entries[1].Name != "shared #2" {
		t.Errorf("duplicate names were not made unique: %q, %q", result.Entries[0].Name, result.Entries[1].Name)
	}
	out, err := f.svc.Clash(result, "https://sub.example.com")
	if err != nil {
		t.Fatalf("Clash: %v", err)
	}
	members := renderedGroups(t, out)["外部线路"]
	if len(members) != 2 || members[0] != "shared" || members[1] != "shared #2" {
		t.Errorf("external group members = %v", members)
	}
	if strings.Contains(out, "secret-path") || strings.Contains(out, shareSource.URL) {
		t.Error("generated subscription leaked the upstream URL")
	}
	decoded, err := base64.StdEncoding.DecodeString(Base64(result))
	if err != nil {
		t.Fatal(err)
	}
	links := strings.Split(strings.TrimSpace(string(decoded)), "\n")
	if len(links) != 1 || !strings.HasPrefix(links[0], "vless://") {
		t.Errorf("base64 output = %q, want only the share-link node", decoded)
	}

	if err := f.db.Model(&second).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	result, err = f.svc.Build(context.Background(), &user)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ExternalSubscriptionID != first.ID {
		t.Fatalf("disabled source still rendered: %+v", result.Entries)
	}

	withoutPlan := model.User{Username: "no plan", UUID: "none", SubToken: "none", Enabled: true}
	result, err = f.svc.Build(context.Background(), &withoutPlan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("user without a profile received external nodes: %+v", result.Entries)
	}
}
