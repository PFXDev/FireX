package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PFXDev/FireX/internal/model"
)

// settingSchemaVersion records which shape the database is in, so a migration
// that already ran is never attempted twice.
const settingSchemaVersion = "schema.version"

// schemaVersion 2 is the routing refactor: nodes became inbounds, classification
// moved onto node groups, and plans stopped picking inbounds in favour of a
// profile whose node-group whitelist decides what a user may reach.
const schemaVersion = 2

// Legacy setting keys, read once during migration and then dropped.
const (
	legacyKeyRouting = "clash.routing"
	legacyKeyMode    = "clash.mode"
)

// migrate brings the database up to schemaVersion. The pre-v2 rename has to
// happen before AutoMigrate, or AutoMigrate would create an empty `inbounds`
// beside the populated `nodes` and strand every row in it.
func (d *DB) migrate(path string) error {
	legacy := d.hasTable("nodes")
	if legacy {
		// A migration that loses a fleet's worth of hand-written labels is not
		// recoverable, so refuse to start rather than proceed without a copy.
		backup, err := d.snapshot(path)
		if err != nil {
			return fmt.Errorf("back up database before migrating: %w", err)
		}
		fmt.Printf("firex: migrating database to schema v%d; backup written to %s\n", schemaVersion, backup)
		if err := d.renameLegacy(); err != nil {
			return err
		}
	}

	if err := d.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if legacy {
		if err := d.fillFromLegacy(); err != nil {
			return fmt.Errorf("migrate legacy data: %w", err)
		}
	}
	return d.SetSetting(settingSchemaVersion, strconv.Itoa(schemaVersion))
}

// snapshot writes a consistent copy of the database next to it. VACUUM INTO
// folds the WAL in, which a plain file copy would miss.
func (d *DB) snapshot(path string) (string, error) {
	target := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405"))
	if err := d.Exec("VACUUM INTO ?", target).Error; err != nil {
		return "", err
	}
	return target, nil
}

func (d *DB) hasTable(name string) bool {
	var count int64
	d.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	return count > 0
}

func (d *DB) hasColumn(table, column string) bool {
	var count int64
	d.Raw(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count)
	return count > 0
}

// renameLegacy carries the old tables over under their new names so the rows
// survive; AutoMigrate then adds whatever columns the new models want.
func (d *DB) renameLegacy() error {
	stmts := []string{`ALTER TABLE nodes RENAME TO inbounds`}
	if d.hasColumn("inbounds", "inbound_id") || d.hasColumn("nodes", "inbound_id") {
		stmts = append(stmts, `ALTER TABLE inbounds RENAME COLUMN inbound_id TO remote_id`)
	}
	if d.hasTable("node_group_nodes") {
		stmts = append(stmts,
			`ALTER TABLE node_group_nodes RENAME TO node_group_inbounds`,
			`ALTER TABLE node_group_inbounds RENAME COLUMN node_id TO inbound_id`,
		)
	}
	for _, stmt := range stmts {
		if err := d.Exec(stmt).Error; err != nil {
			return fmt.Errorf("rename legacy schema (%s): %w", stmt, err)
		}
	}
	return nil
}

// fillFromLegacy moves the data that changed shape rather than name. Order
// matters: groups have to exist before profiles can whitelist them.
func (d *DB) fillFromLegacy() error {
	if err := d.legacyGroupTags(); err != nil {
		return err
	}
	if err := d.legacyRegionGroups(); err != nil {
		return err
	}
	if err := d.legacyRouting(); err != nil {
		return err
	}
	if err := d.legacyProfiles(); err != nil {
		return err
	}
	d.dropLegacyColumns()
	return nil
}

// legacyGroupTags turns the old fixed region/line columns into key/value tags.
func (d *DB) legacyGroupTags() error {
	if !d.hasColumn("node_groups", "region") {
		return nil
	}
	line := "line"
	if !d.hasColumn("node_groups", "line") {
		line = "''"
	}
	var rows []struct {
		ID     uint
		Region string
		Line   string
	}
	query := fmt.Sprintf(`SELECT id, COALESCE(region, '') AS region, COALESCE(%s, '') AS line FROM node_groups`, line)
	if err := d.Raw(query).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		for _, tag := range []struct{ key, value string }{
			{model.TagKeyRegion, row.Region},
			{model.TagKeyLine, row.Line},
		} {
			value := strings.TrimSpace(tag.value)
			if value == "" {
				continue
			}
			if err := d.Create(&model.NodeGroupTag{GroupID: row.ID, Key: tag.key, Value: value}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyRegionGroups gives every inbound that belongs to no group a home, using
// the region text the old schema kept on the inbound itself. Without this an
// operator who never built groups would find their plans covering nothing.
func (d *DB) legacyRegionGroups() error {
	if !d.hasColumn("inbounds", "region") {
		return nil
	}
	var rows []struct {
		ID     uint
		Region string
	}
	err := d.Raw(`
		SELECT i.id, COALESCE(i.region, '') AS region
		FROM inbounds i
		WHERE NOT EXISTS (SELECT 1 FROM node_group_inbounds m WHERE m.inbound_id = i.id)
		ORDER BY i.sort_order ASC, i.id ASC`).Scan(&rows).Error
	if err != nil {
		return err
	}

	var order []string
	members := map[string][]uint{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Region)
		if name == "" {
			name = "未分组"
		}
		if _, seen := members[name]; !seen {
			order = append(order, name)
		}
		members[name] = append(members[name], row.ID)
	}

	for i, name := range order {
		group, err := d.groupByName(name, 200+i)
		if err != nil {
			return err
		}
		for _, inboundID := range members[name] {
			if err := d.Create(&model.NodeGroupInbound{GroupID: group.ID, InboundID: inboundID}).Error; err != nil {
				return err
			}
		}
		if region := strings.TrimSpace(name); region != "未分组" {
			d.Create(&model.NodeGroupTag{GroupID: group.ID, Key: model.TagKeyRegion, Value: region})
		}
	}
	return nil
}

// groupByName returns the node group with this name, creating it if needed. A
// name the operator already used is reused rather than duplicated.
func (d *DB) groupByName(name string, sortOrder int) (*model.NodeGroup, error) {
	var group model.NodeGroup
	if err := d.First(&group, "name = ?", name).Error; err == nil {
		return &group, nil
	} else if !IsNotFound(err) {
		return nil, err
	}
	group = model.NodeGroup{
		Name: name, Type: model.GroupTypeURLTest,
		Interval: 300, Tolerance: 50, Multiplier: 1,
		SortOrder: sortOrder, Enabled: true,
	}
	if err := d.Create(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// ---------------------------------------------------------------- routing

type legacyMember struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type legacyGroup struct {
	Name      string         `json:"name"`
	Icon      string         `json:"icon"`
	Type      string         `json:"type"`
	Members   []legacyMember `json:"members"`
	TestURL   string         `json:"testUrl"`
	Interval  int            `json:"interval"`
	Tolerance int            `json:"tolerance"`
}

type legacyRule struct {
	Type      string       `json:"type"`
	Value     string       `json:"value"`
	Target    legacyMember `json:"target"`
	NoResolve bool         `json:"noResolve"`
	Disabled  bool         `json:"disabled"`
}

type legacyRoutingDoc struct {
	Groups []legacyGroup `json:"groups"`
	Rules  []legacyRule  `json:"rules"`
	Final  legacyMember  `json:"final"`
}

// legacyRouting splits the single stored routing blob into policies (the rule
// lists) and their default-column egresses. A blob that is missing or corrupt
// is left to routing.Seed, which installs the stock set instead.
func (d *DB) legacyRouting() error {
	raw := d.GetSetting(legacyKeyRouting, "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var doc legacyRoutingDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || len(doc.Groups) == 0 {
		return nil
	}

	// Every legacy policy group becomes a policy; its member list becomes the
	// default egress, which is what every profile falls back to.
	byName := map[string]*model.Policy{}
	for _, group := range doc.Groups {
		name := strings.TrimSpace(group.Name)
		if name == "" || byName[name] != nil {
			continue
		}
		policy := &model.Policy{Name: name, Icon: strings.TrimSpace(group.Icon), Enabled: true}
		if err := d.Create(policy).Error; err != nil {
			return err
		}
		byName[name] = policy
		if err := d.writeDefaultEgress(policy.ID, group); err != nil {
			return err
		}
	}

	// Rules belonged to a target; now they belong to the policy that target
	// names. A rule that pointed straight at DIRECT gets a policy synthesised
	// for it so it still has somewhere to live.
	firstRule := map[uint]int{}
	counts := map[uint]int{}
	for i, rule := range doc.Rules {
		policy, err := d.policyForMember(rule.Target, byName)
		if err != nil {
			return err
		}
		if policy == nil {
			continue
		}
		if _, seen := firstRule[policy.ID]; !seen {
			firstRule[policy.ID] = i
		}
		row := model.Rule{
			PolicyID:  policy.ID,
			SortOrder: counts[policy.ID],
			Type:      rule.Type,
			Value:     rule.Value,
			NoResolve: rule.NoResolve,
			Disabled:  rule.Disabled,
		}
		counts[policy.ID]++
		if err := d.Create(&row).Error; err != nil {
			return err
		}
	}

	final, err := d.policyForMember(doc.Final, byName)
	if err != nil {
		return err
	}
	if final != nil {
		d.Model(final).Update("is_final", true)
	}

	// One order now serves both rule precedence and the client's group list.
	// Rule-less policies are the manual switches, so they lead; the rest follow
	// in the order their first rule appeared; the MATCH target sits last.
	policies := make([]*model.Policy, 0, len(byName))
	for _, policy := range byName {
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(i, j int) bool {
		a, b := policies[i], policies[j]
		ai, aHas := firstRule[a.ID]
		bi, bHas := firstRule[b.ID]
		if aHas != bHas {
			return !aHas
		}
		if aHas && ai != bi {
			return ai < bi
		}
		return a.ID < b.ID
	})
	next := 0
	for _, policy := range policies {
		if final != nil && policy.ID == final.ID {
			continue
		}
		next += 10
		d.Model(policy).Update("sort_order", next)
	}
	if final != nil {
		d.Model(final).Update("sort_order", next+10)
	}

	d.SetSetting(legacyKeyRouting, "")
	d.SetSetting(legacyKeyMode, "")
	return nil
}

func (d *DB) writeDefaultEgress(policyID uint, group legacyGroup) error {
	egress := model.Egress{
		PolicyID:  policyID,
		ProfileID: model.DefaultProfileID,
		Type:      group.Type,
		TestURL:   group.TestURL,
		Interval:  group.Interval,
		Tolerance: group.Tolerance,
	}
	if egress.Type == "" {
		egress.Type = model.GroupTypeSelect
	}
	if err := d.Create(&egress).Error; err != nil {
		return err
	}
	for i, member := range group.Members {
		kind, ok := convertMemberKind(member.Kind)
		if !ok {
			continue
		}
		row := model.EgressMember{EgressID: egress.ID, SortOrder: i, Kind: kind, Ref: member.Ref}
		if err := d.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func convertMemberKind(kind string) (string, bool) {
	switch kind {
	case "policy":
		return model.MemberPolicy, true
	case "node-group":
		return model.MemberNodeGroup, true
	case "builtin":
		return model.MemberBuiltin, true
	case "all-groups":
		return model.MemberAllNodeGroups, true
	case "all-nodes":
		return model.MemberAllInbounds, true
	}
	return "", false
}

// policyForMember finds — or invents — the policy a legacy rule target maps to.
func (d *DB) policyForMember(member legacyMember, byName map[string]*model.Policy) (*model.Policy, error) {
	if member.Kind == "policy" {
		return byName[strings.TrimSpace(member.Ref)], nil
	}
	kind, ok := convertMemberKind(member.Kind)
	if !ok {
		return nil, nil
	}
	name := strings.TrimSpace(member.Ref)
	if name == "" {
		name = kind
	}
	if existing := byName[name]; existing != nil {
		return existing, nil
	}
	policy := &model.Policy{Name: name, Enabled: true}
	if err := d.Create(policy).Error; err != nil {
		return nil, err
	}
	byName[name] = policy
	egress := model.Egress{PolicyID: policy.ID, ProfileID: model.DefaultProfileID, Type: model.GroupTypeSelect}
	if err := d.Create(&egress).Error; err != nil {
		return nil, err
	}
	err := d.Create(&model.EgressMember{EgressID: egress.ID, Kind: kind, Ref: member.Ref}).Error
	return policy, err
}

// ---------------------------------------------------------------- profiles

// legacyProfiles turns each plan's hand-picked inbound set into a profile whose
// node-group whitelist grants exactly the same inbounds — never more. A cheap
// plan silently gaining a premium line on the first reconcile after an upgrade
// would be worse than any amount of migration clutter, so groups are only
// whitelisted when every one of their members was already in the plan, and
// whatever that leaves uncovered gets a group of its own.
func (d *DB) legacyProfiles() error {
	if !d.hasTable("plan_nodes") {
		return nil
	}
	var plans []model.Plan
	if err := d.Order("sort_order ASC, id ASC").Find(&plans).Error; err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}

	groupMembers, err := d.groupMembership()
	if err != nil {
		return err
	}

	for i := range plans {
		plan := &plans[i]
		var rows []struct{ NodeID uint }
		err := d.Raw(`
			SELECT pn.node_id FROM plan_nodes pn
			JOIN inbounds i ON i.id = pn.node_id
			WHERE pn.plan_id = ?`, plan.ID).Scan(&rows).Error
		if err != nil {
			return err
		}
		inboundIDs := make([]uint, 0, len(rows))
		granted := map[uint]bool{}
		for _, row := range rows {
			inboundIDs = append(inboundIDs, row.NodeID)
			granted[row.NodeID] = true
		}

		profile := model.Profile{Name: plan.Name, SortOrder: plan.SortOrder, Enabled: true}
		if err := d.Create(&profile).Error; err != nil {
			// A plan and a node group may share a name; the profile name is
			// unique on its own, so only a plan-name clash can land here.
			profile = model.Profile{Name: fmt.Sprintf("%s #%d", plan.Name, plan.ID), SortOrder: plan.SortOrder, Enabled: true}
			if err := d.Create(&profile).Error; err != nil {
				return err
			}
		}

		covered := map[uint]bool{}
		for groupID, members := range groupMembers {
			if len(members) == 0 || !subsetOf(members, granted) {
				continue
			}
			if err := d.Create(&model.ProfileNodeGroup{ProfileID: profile.ID, GroupID: groupID}).Error; err != nil {
				return err
			}
			for _, id := range members {
				covered[id] = true
			}
		}

		var leftover []uint
		for _, id := range inboundIDs {
			if !covered[id] {
				leftover = append(leftover, id)
			}
		}
		if len(leftover) > 0 {
			group, err := d.groupByName(fmt.Sprintf("%s 其余节点", plan.Name), 900+i)
			if err != nil {
				return err
			}
			for _, id := range leftover {
				d.Create(&model.NodeGroupInbound{GroupID: group.ID, InboundID: id})
			}
			if err := d.Create(&model.ProfileNodeGroup{ProfileID: profile.ID, GroupID: group.ID}).Error; err != nil {
				return err
			}
			groupMembers[group.ID] = leftover
		}

		if err := d.Model(plan).Update("profile_id", profile.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) groupMembership() (map[uint][]uint, error) {
	var links []model.NodeGroupInbound
	if err := d.Find(&links).Error; err != nil {
		return nil, err
	}
	out := map[uint][]uint{}
	for _, link := range links {
		out[link.GroupID] = append(out[link.GroupID], link.InboundID)
	}
	return out, nil
}

func subsetOf(members []uint, granted map[uint]bool) bool {
	for _, id := range members {
		if !granted[id] {
			return false
		}
	}
	return true
}

// dropLegacyColumns is best effort: a column left behind is dead weight, not a
// bug, and older SQLite builds cannot drop columns at all.
func (d *DB) dropLegacyColumns() {
	for _, stmt := range []string{
		`ALTER TABLE inbounds DROP COLUMN region`,
		`ALTER TABLE inbounds DROP COLUMN tags`,
		`ALTER TABLE inbounds DROP COLUMN multiplier`,
		`ALTER TABLE node_groups DROP COLUMN region`,
		`ALTER TABLE node_groups DROP COLUMN line`,
	} {
		d.Exec(stmt)
	}
}
