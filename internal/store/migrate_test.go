package store_test

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
)

// legacySchema is the pre-v2 shape, cut down to the columns the migration
// reads. Building it by hand is the only way to prove an existing install
// survives the upgrade.
const legacySchema = `
CREATE TABLE panels (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, base_url TEXT,
  api_token TEXT, skip_tls_verify NUMERIC, enabled NUMERIC, remark TEXT, status TEXT,
  last_error TEXT, last_seen_at INTEGER, xray_version TEXT, created_at INTEGER, updated_at INTEGER);
CREATE TABLE nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, panel_id INTEGER, inbound_id INTEGER,
  inbound_tag TEXT, protocol TEXT, port INTEGER, remote_remark TEXT, remote_enabled NUMERIC,
  name TEXT, region TEXT, emoji TEXT, tags TEXT, sort_order INTEGER, enabled NUMERIC,
  udp NUMERIC, multiplier REAL, missing NUMERIC, last_seen_at INTEGER,
  created_at INTEGER, updated_at INTEGER);
CREATE TABLE node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, emoji TEXT,
  region TEXT, line TEXT, type TEXT, test_url TEXT, interval INTEGER, tolerance INTEGER,
  sort_order INTEGER, enabled NUMERIC, remark TEXT, created_at INTEGER, updated_at INTEGER);
CREATE TABLE node_group_nodes (group_id INTEGER, node_id INTEGER);
CREATE TABLE plans (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, traffic_bytes INTEGER,
  duration_days INTEGER, device_limit INTEGER, speed_note TEXT, enabled NUMERIC,
  sort_order INTEGER, remark TEXT, created_at INTEGER, updated_at INTEGER);
CREATE TABLE plan_nodes (plan_id INTEGER, node_id INTEGER);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
`

// legacyRoutingJSON is what the old visual editor stored: one policy group with
// members, one rule pointing at it, and a final target.
const legacyRoutingJSON = `{
  "groups": [
    {"name":"节点选择","icon":"🚀","type":"select","members":[{"kind":"all-groups","ref":""},{"kind":"builtin","ref":"DIRECT"}]},
    {"name":"AI 服务","icon":"🤖","type":"select","members":[{"kind":"policy","ref":"节点选择"}]},
    {"name":"漏网之鱼","icon":"🐟","type":"select","members":[{"kind":"policy","ref":"节点选择"}]}
  ],
  "rules": [
    {"type":"GEOSITE","value":"openai","target":{"kind":"policy","ref":"AI 服务"}},
    {"type":"GEOIP","value":"cn","target":{"kind":"policy","ref":"节点选择"},"noResolve":true}
  ],
  "final": {"kind":"policy","ref":"漏网之鱼"}
}`

// seedLegacy writes a pre-v2 database: one panel, three inbounds, one hand-made
// group covering two of them, and two plans with different inbound sets.
func seedLegacy(t *testing.T, path string) {
	t.Helper()
	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := raw.Exec(legacySchema).Error; err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	stmts := []string{
		`INSERT INTO panels (id, name, base_url, api_token, enabled) VALUES (1, 'p1', 'http://x', 'tok', 1)`,
		`INSERT INTO nodes (id, panel_id, inbound_id, port, protocol, name, region, tags, emoji, sort_order, enabled, udp, multiplier, missing)
		   VALUES (1, 1, 11, 443, 'vless', 'HK 01', '🇭🇰 香港', 'premium', '🇭🇰', 1, 1, 1, 1, 0)`,
		`INSERT INTO nodes (id, panel_id, inbound_id, port, protocol, name, region, tags, emoji, sort_order, enabled, udp, multiplier, missing)
		   VALUES (2, 1, 12, 8443, 'vless', 'HK 02', '🇭🇰 香港', '', '🇭🇰', 2, 1, 1, 1, 0)`,
		`INSERT INTO nodes (id, panel_id, inbound_id, port, protocol, name, region, tags, emoji, sort_order, enabled, udp, multiplier, missing)
		   VALUES (3, 1, 13, 9443, 'vless', 'JP 01', '🇯🇵 日本', '', '🇯🇵', 3, 1, 1, 1, 0)`,
		`INSERT INTO node_groups (id, name, emoji, region, line, type, interval, tolerance, sort_order, enabled)
		   VALUES (1, '香港 IEPL', '🇭🇰', '香港', 'IEPL', 'url-test', 300, 50, 1, 1)`,
		`INSERT INTO node_group_nodes (group_id, node_id) VALUES (1, 1)`,
		`INSERT INTO node_group_nodes (group_id, node_id) VALUES (1, 2)`,
		`INSERT INTO plans (id, name, enabled, sort_order) VALUES (1, 'vip', 1, 1)`,
		`INSERT INTO plans (id, name, enabled, sort_order) VALUES (2, 'basic', 1, 2)`,
		// vip reaches every inbound; basic only the first.
		`INSERT INTO plan_nodes (plan_id, node_id) VALUES (1, 1)`,
		`INSERT INTO plan_nodes (plan_id, node_id) VALUES (1, 2)`,
		`INSERT INTO plan_nodes (plan_id, node_id) VALUES (1, 3)`,
		`INSERT INTO plan_nodes (plan_id, node_id) VALUES (2, 1)`,
	}
	for _, stmt := range stmts {
		if err := raw.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy row: %v\n%s", err, stmt)
		}
	}
	if err := raw.Exec(`INSERT INTO settings (key, value) VALUES ('clash.routing', ?)`, legacyRoutingJSON).Error; err != nil {
		t.Fatalf("seed legacy routing: %v", err)
	}
	sqlDB, _ := raw.DB()
	sqlDB.Close()
}

func migrateLegacy(t *testing.T) (*store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "firex.db")
	seedLegacy(t, path)
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatalf("store.Open() on a legacy database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestMigrationRenamesInboundsAndKeepsLabels(t *testing.T) {
	db, _ := migrateLegacy(t)

	var inbounds []model.Inbound
	if err := db.Order("remote_id ASC").Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if len(inbounds) != 3 {
		t.Fatalf("inbounds = %d, want 3", len(inbounds))
	}
	if inbounds[0].RemoteID != 11 || inbounds[0].Name != "HK 01" || inbounds[0].Emoji != "🇭🇰" {
		t.Errorf("inbound[0] = %+v, want the panel id and admin labels carried over", inbounds[0])
	}
	if !inbounds[0].Enabled {
		t.Error("enabled flag lost in the rename")
	}
}

func TestMigrationBacksUpBeforeTouchingAnything(t *testing.T) {
	_, path := migrateLegacy(t)
	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("backups = %v (err %v), want exactly one snapshot", matches, err)
	}
}

func TestMigrationMovesGroupLabelsToTags(t *testing.T) {
	db, _ := migrateLegacy(t)

	var tags []model.NodeGroupTag
	db.Where("group_id = ?", 1).Order("key ASC").Find(&tags)
	got := map[string]string{}
	for _, tag := range tags {
		got[tag.Key] = tag.Value
	}
	if got[model.TagKeyRegion] != "香港" || got[model.TagKeyLine] != "IEPL" {
		t.Errorf("tags = %v, want the old region and line columns", got)
	}
}

// An inbound in no group would be unreachable after the upgrade, so the old
// region text becomes a group of its own.
func TestMigrationGroupsUncoveredInbounds(t *testing.T) {
	db, _ := migrateLegacy(t)

	var group model.NodeGroup
	if err := db.First(&group, "name = ?", "🇯🇵 日本").Error; err != nil {
		t.Fatalf("no group generated for the ungrouped inbound: %v", err)
	}
	var count int64
	db.Model(&model.NodeGroupInbound{}).Where("group_id = ?", group.ID).Count(&count)
	if count != 1 {
		t.Errorf("members = %d, want the one JP inbound", count)
	}
	// The two inbounds the operator had already grouped must not be duplicated
	// into a region group.
	var hk int64
	db.Model(&model.NodeGroup{}).Where("name = ?", "🇭🇰 香港").Count(&hk)
	if hk != 0 {
		t.Error("a region group was generated for inbounds that were already grouped")
	}
}

func TestMigrationSplitsRoutingIntoPolicies(t *testing.T) {
	db, _ := migrateLegacy(t)

	var policies []model.Policy
	db.Order("sort_order ASC").Find(&policies)
	names := make([]string, 0, len(policies))
	for _, p := range policies {
		names = append(names, p.Name)
	}
	if len(policies) != 3 {
		t.Fatalf("policies = %v, want one per legacy group", names)
	}
	// One order now serves both rule precedence and the client's group list, so
	// a policy takes the position of its first rule in the old flat list and the
	// MATCH target sits last whatever it held before.
	if names[0] != "AI 服务" || names[1] != "节点选择" {
		t.Errorf("policy order = %v, want the old rule order preserved", names)
	}
	if names[len(names)-1] != "漏网之鱼" || !policies[len(policies)-1].IsFinal {
		t.Errorf("policy order = %v, want the final policy last", names)
	}

	var rules []model.Rule
	db.Find(&rules)
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want both legacy rules kept", len(rules))
	}
	for _, rule := range rules {
		var owner model.Policy
		db.First(&owner, rule.PolicyID)
		if rule.Value == "openai" && owner.Name != "AI 服务" {
			t.Errorf("rule %q landed under %q, want its old target", rule.Value, owner.Name)
		}
	}

	// The default column must carry the old member lists, with the renamed kind.
	var members []model.EgressMember
	db.Find(&members)
	found := false
	for _, m := range members {
		if m.Kind == model.MemberAllNodeGroups {
			found = true
		}
	}
	if !found {
		t.Errorf("members = %+v, want all-groups converted to all-node-groups", members)
	}
	if raw := db.GetSetting("clash.routing", ""); raw != "" {
		t.Error("the legacy routing blob was left behind")
	}
}

// The migration must never widen what a plan reaches. A cheap plan silently
// gaining a premium line on the first reconcile would be worse than any amount
// of migration clutter.
func TestMigrationPreservesEachPlansInboundSet(t *testing.T) {
	db, _ := migrateLegacy(t)

	want := map[string][]int{
		"vip":   {11, 12, 13},
		"basic": {11},
	}
	for name, remoteIDs := range want {
		var plan model.Plan
		if err := db.First(&plan, "name = ?", name).Error; err != nil {
			t.Fatalf("plan %q missing: %v", name, err)
		}
		if plan.ProfileID == 0 {
			t.Fatalf("plan %q was not bound to a profile", name)
		}
		inbounds, err := routing.InboundsForProfile(db, plan.ProfileID)
		if err != nil {
			t.Fatalf("InboundsForProfile(%q): %v", name, err)
		}
		got := make([]int, 0, len(inbounds))
		for _, n := range inbounds {
			got = append(got, n.RemoteID)
		}
		if len(got) != len(remoteIDs) {
			t.Fatalf("plan %q reaches %v, want exactly %v", name, got, remoteIDs)
		}
		for i := range got {
			if got[i] != remoteIDs[i] {
				t.Errorf("plan %q reaches %v, want %v", name, got, remoteIDs)
				break
			}
		}
	}
}

func TestMigrationIsNotRepeated(t *testing.T) {
	db, path := migrateLegacy(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again, err := store.Open(path, false)
	if err != nil {
		t.Fatalf("second store.Open(): %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })

	var groups int64
	again.Model(&model.NodeGroup{}).Count(&groups)
	// The hand-made 香港 IEPL, the 🇯🇵 日本 built from region text, and the
	// leftover group that keeps `basic` reaching exactly what it used to.
	if groups != 3 {
		t.Errorf("node groups = %d after reopening, want the 3 the migration produced", groups)
	}
	matches, _ := filepath.Glob(path + ".bak-*")
	if len(matches) != 1 {
		t.Errorf("backups = %d, want no second snapshot on a migrated database", len(matches))
	}
}

// A fresh install must come up clean, with no backup file and no legacy work.
func TestFreshDatabaseNeedsNoMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Errorf("fresh install wrote a backup: %v", matches)
	}
	var inbounds int64
	db.Model(&model.Inbound{}).Count(&inbounds)
	if inbounds != 0 {
		t.Errorf("inbounds = %d on a fresh database", inbounds)
	}
}
