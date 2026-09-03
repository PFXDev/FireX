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
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
)

type fixture struct {
	db      *store.DB
	mgr     *provision.Manager
	svc     *Service
	fake    *paneltest.Panel
	user    *model.User
	profile *model.Profile
	groups  []model.NodeGroup
}

// newFixture provisions one user across a two-inbound panel, one node group per
// inbound, and returns everything a subscription test needs.
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
	if err := routing.Seed(db); err != nil {
		t.Fatalf("routing.Seed() error = %v", err)
	}

	p := model.Panel{Name: "p1", BaseURL: fake.URL(), APIToken: "tok", Enabled: true}
	db.Create(&p)

	mgr := provision.NewManager(db)
	ctx := context.Background()
	if err := mgr.DiscoverPanel(ctx, &p); err != nil {
		t.Fatalf("DiscoverPanel() error = %v", err)
	}

	var inbounds []model.Inbound
	db.Order("remote_id ASC").Find(&inbounds)
	labels := []struct{ name, emoji, group string }{
		{"HK 01", "🇭🇰", "香港 IEPL"},
		{"JP 01", "🇯🇵", "日本 直连"},
	}
	profile := model.Profile{Name: "all", Enabled: true}
	db.Create(&profile)

	var groups []model.NodeGroup
	for i := range inbounds {
		inbounds[i].Enabled = true
		inbounds[i].Name = labels[i].name
		inbounds[i].Emoji = labels[i].emoji
		inbounds[i].SortOrder = i + 1
		db.Save(&inbounds[i])

		g := model.NodeGroup{
			Name: labels[i].group, Type: model.GroupTypeURLTest,
			Multiplier: 1, SortOrder: i + 1, Enabled: true,
		}
		db.Create(&g)
		db.Create(&model.NodeGroupInbound{GroupID: g.ID, InboundID: inbounds[i].ID})
		db.Create(&model.ProfileNodeGroup{ProfileID: profile.ID, GroupID: g.ID})
		groups = append(groups, g)
	}

	plan := model.Plan{Name: "all", ProfileID: profile.ID, Enabled: true}
	db.Create(&plan)

	u := model.User{
		Username: "bob", UUID: "uuid-bob", SubToken: "tok-bob",
		PlanID: plan.ID, Enabled: true, TrafficLimit: 10 << 30,
	}
	db.Create(&u)
	if err := mgr.ReconcileUser(ctx, &u); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	return &fixture{db: db, mgr: mgr, svc: NewService(db, mgr), fake: fake, user: &u, profile: &profile, groups: groups}
}

func TestBuildNamesEntriesFromInbounds(t *testing.T) {
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
		t.Errorf("entry[0].Name = %q, want the inbound's emoji + name", result.Entries[0].Name)
	}
	// Port is the only handle linking a link back to its inbound.
	if result.Entries[1].Inbound.Port != 8443 {
		t.Errorf("entry[1] matched inbound port %d, want 8443", result.Entries[1].Inbound.Port)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", result.Warnings)
	}
}

func renderedGroups(t *testing.T, out string) map[string][]string {
	t.Helper()
	var cfg struct {
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("rendered profile is not valid YAML: %v\n%s", err, out)
	}
	groups := map[string][]string{}
	for _, g := range cfg.Groups {
		groups[g.Name] = g.Proxies
	}
	return groups
}

func TestClashOutputRendersNodeGroupsAndPolicies(t *testing.T) {
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
			Type string `yaml:"type"`
			UUID string `yaml:"uuid"`
		} `yaml:"proxies"`
		Rules []string `yaml:"rules"`
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

	groups := renderedGroups(t, out)
	if got := groups["香港 IEPL"]; len(got) != 1 || got[0] != "🇭🇰 HK 01" {
		t.Errorf("香港 IEPL members = %v, want just the HK proxy", got)
	}
	// A policy's all-node-groups member expands to the profile's whitelist.
	if got := groups["🚀 节点选择"]; !containsAll(got, "香港 IEPL", "日本 直连") {
		t.Errorf("节点选择 members = %v, want both node groups", got)
	}
	if len(cfg.Rules) == 0 || cfg.Rules[len(cfg.Rules)-1] != "MATCH,🐟 漏网之鱼" {
		t.Errorf("rules end with %v, want the final policy", cfg.Rules)
	}
}

// A group the profile does not whitelist must not reach the client at all — not
// its proxies, not its group.
func TestClashOutputHonoursProfileWhitelist(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.db.Where("profile_id = ? AND group_id = ?", f.profile.ID, f.groups[1].ID).
		Delete(&model.ProfileNodeGroup{})
	// The API reconciles and drops the cache on a whitelist edit; without it the
	// panel would still hand back a link for the inbound the user just lost.
	if err := f.mgr.ReconcileUser(ctx, f.user); err != nil {
		t.Fatalf("ReconcileUser() error = %v", err)
	}
	f.svc.InvalidateUser(f.user)

	result, err := f.svc.Build(ctx, f.user)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %d, want only the whitelisted group's inbound", len(result.Entries))
	}
	out, err := f.svc.Clash(result)
	if err != nil {
		t.Fatalf("Clash() error = %v", err)
	}
	groups := renderedGroups(t, out)
	if _, ok := groups["日本 直连"]; ok {
		t.Errorf("a group outside the profile rendered: %v", groups)
	}
	if got := groups["香港 IEPL"]; len(got) != 1 {
		t.Errorf("香港 IEPL members = %v", got)
	}
}

func containsAll(list []string, want ...string) bool {
	seen := map[string]bool{}
	for _, v := range list {
		seen[v] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
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
		t.Errorf("link[0] = %q, want the FireX display name in the fragment", links[0])
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

func TestClashHonoursPerInboundUdpToggle(t *testing.T) {
	f := newFixture(t)
	// The inbound UDP switch is the admin's only lever here; the panel's share
	// link says nothing about UDP.
	f.db.Model(&model.Inbound{}).Where("port = ?", 8443).Update("udp", false)

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
		t.Errorf("proxy[1] udp = true, want the inbound's toggle respected")
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
