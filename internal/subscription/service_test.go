package subscription

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
)

type fixture struct {
	db   *store.DB
	mgr  *provision.Manager
	svc  *Service
	fake *paneltest.Panel
	user *model.User
}

// newFixture provisions one user across a two-inbound panel and returns
// everything a subscription test needs.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	fake := paneltest.New("tok",
		paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "hk-raw", Enable: true},
		paneltest.Inbound{ID: 2, Port: 8443, Protocol: "vless", Remark: "jp-raw", Enable: true},
	)
	t.Cleanup(fake.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p := model.Panel{Name: "p1", BaseURL: fake.URL(), APIToken: "tok", Enabled: true}
	db.Create(&p)

	mgr := provision.NewManager(db)
	ctx := context.Background()
	if err := mgr.DiscoverPanel(ctx, &p); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var nodes []model.Node
	db.Order("inbound_id ASC").Find(&nodes)
	labels := []struct{ name, region, emoji, tags string }{
		{"HK 01", "🇭🇰 香港", "🇭🇰", "media,premium"},
		{"JP 01", "🇯🇵 日本", "🇯🇵", "basic"},
	}
	plan := model.Plan{Name: "all", Enabled: true}
	db.Create(&plan)
	for i := range nodes {
		nodes[i].Enabled = true
		nodes[i].Name = labels[i].name
		nodes[i].Region = labels[i].region
		nodes[i].Emoji = labels[i].emoji
		nodes[i].Tags = labels[i].tags
		nodes[i].SortOrder = i + 1
		db.Save(&nodes[i])
		db.Create(&model.PlanNode{PlanID: plan.ID, NodeID: nodes[i].ID})
	}

	u := model.User{
		Username: "bob", UUID: "uuid-bob", SubToken: "tok-bob",
		PlanID: plan.ID, Enabled: true, TrafficLimit: 10 << 30,
	}
	db.Create(&u)
	if err := mgr.ReconcileUser(ctx, &u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	return &fixture{db: db, mgr: mgr, svc: NewService(db, mgr), fake: fake, user: &u}
}

func TestBuildNamesEntriesFromNodes(t *testing.T) {
	f := newFixture(t)
	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (warnings: %v)", len(result.Entries), result.Warnings)
	}
	// The panel's own remark must not leak through; FireX owns the display name.
	if result.Entries[0].Name != "🇭🇰 HK 01" {
		t.Errorf("entry[0].Name = %q, want the node's emoji + name", result.Entries[0].Name)
	}
	if result.Entries[0].Node.Region != "🇭🇰 香港" {
		t.Errorf("entry[0] region = %q", result.Entries[0].Node.Region)
	}
	// Port is the only handle linking a link back to its inbound.
	if result.Entries[1].Node.Port != 8443 {
		t.Errorf("entry[1] matched node port %d, want 8443", result.Entries[1].Node.Port)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", result.Warnings)
	}
}

func TestClashOutputGroupsByRegion(t *testing.T) {
	f := newFixture(t)
	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	out, err := f.svc.Clash(result)
	if err != nil {
		t.Fatalf("Clash() error = %v", err)
	}
	var cfg struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Type   string `yaml:"type"`
			Server string `yaml:"server"`
			UUID   string `yaml:"uuid"`
		} `yaml:"proxies"`
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("rendered profile is not valid YAML: %v\n%s", err, out)
	}
	if len(cfg.Proxies) != 2 {
		t.Fatalf("proxies = %d, want 2", len(cfg.Proxies))
	}
	if cfg.Proxies[0].UUID != "uuid-bob" || cfg.Proxies[0].Type != "vless" {
		t.Errorf("proxy[0] = %+v", cfg.Proxies[0])
	}
	var names []string
	for _, g := range cfg.Groups {
		names = append(names, g.Name)
	}
	for _, want := range []string{"🇭🇰 香港", "🇯🇵 日本"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("region group %q missing from %v", want, names)
		}
	}
}

func TestBase64OutputCarriesRenamedLinks(t *testing.T) {
	f := newFixture(t)
	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(Base64(result))
	if err != nil {
		t.Fatalf("output is not base64: %v", err)
	}
	links := strings.Split(strings.TrimSpace(string(decoded)), "\n")
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2:\n%s", len(links), decoded)
	}
	for _, link := range links {
		if !strings.HasPrefix(link, "vless://uuid-bob@") {
			t.Errorf("link %q does not carry the user's uuid", link)
		}
	}
	// The fragment is what v2rayN-style clients display.
	if !strings.Contains(links[0], "%F0%9F%87%AD%F0%9F%87%B0%20HK%2001") {
		t.Errorf("link[0] = %q, want the FireX node name in the fragment", links[0])
	}
}

func TestBuildReportsPanelFailureWithoutLosingOtherPanels(t *testing.T) {
	f := newFixture(t)
	f.fake.FailNext["/clients/links/bob@firex"] = true
	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want the panel failure recorded", result.Warnings)
	}
	// Rendering must still produce a loadable profile, not an error page.
	out, err := f.svc.Clash(result)
	if err != nil {
		t.Fatalf("Clash() error = %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("degraded profile is not valid YAML: %v", err)
	}
}

func TestLinksAreCachedPerPanel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Build(ctx, f.user); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	f.fake.ResetCalls()
	if _, err := f.svc.Build(ctx, f.user); err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	for _, call := range f.fake.Calls() {
		if strings.Contains(call, "/clients/links/") {
			t.Errorf("link fetch repeated within the cache window: %v", f.fake.Calls())
		}
	}

	// An admin edit has to be visible immediately, not a minute later.
	f.svc.InvalidateUser(f.user)
	f.fake.ResetCalls()
	if _, err := f.svc.Build(ctx, f.user); err != nil {
		t.Fatalf("third Build() error = %v", err)
	}
	refetched := false
	for _, call := range f.fake.Calls() {
		if strings.Contains(call, "/clients/links/") {
			refetched = true
		}
	}
	if !refetched {
		t.Errorf("InvalidateUser did not force a refetch: %v", f.fake.Calls())
	}
}

func TestClashHonoursPerNodeUdpToggle(t *testing.T) {
	f := newFixture(t)
	// The node UDP switch is the admin's only lever here; the panel's share
	// link says nothing about UDP.
	f.db.Model(&model.Node{}).Where("port = ?", 8443).Update("udp", false)

	result, err := f.svc.Build(context.Background(), f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	out, err := f.svc.Clash(result)
	if err != nil {
		t.Fatalf("Clash() error = %v", err)
	}
	var cfg struct {
		Proxies []struct {
			Name string `yaml:"name"`
			UDP  bool   `yaml:"udp"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}
	if len(cfg.Proxies) != 2 {
		t.Fatalf("proxies = %d, want 2", len(cfg.Proxies))
	}
	if !cfg.Proxies[0].UDP {
		t.Errorf("proxy[0] udp = false, want true")
	}
	if cfg.Proxies[1].UDP {
		t.Errorf("proxy[1] udp = true, want the node's toggle respected")
	}
}

func TestUserInfoHeader(t *testing.T) {
	u := &model.User{Upload: 100, Download: 250, TrafficLimit: 1000, ExpiresAt: 1893456000000}
	got := UserInfo(u)
	want := "upload=100; download=250; total=1000; expire=1893456000"
	if got != want {
		t.Errorf("UserInfo() = %q, want %q", got, want)
	}
}

func TestUserInfoOmitsExpiryWhenUnset(t *testing.T) {
	got := UserInfo(&model.User{Upload: 1, Download: 2, TrafficLimit: 0})
	if strings.Contains(got, "expire") {
		t.Errorf("UserInfo() = %q, want no expire field for a never-expiring user", got)
	}
}
