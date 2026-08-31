package subscription

import (
	"encoding/json"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// Settings that decide how a subscription is rendered.
const (
	// SettingKeyRouting holds the visual routing model as JSON.
	SettingKeyRouting = "clash.routing"
	// SettingKeyMode selects which of the two editors owns proxy-groups and
	// rules. The template is always the base config either way.
	SettingKeyMode = "clash.mode"
)

// Rendering modes.
const (
	// ModeVisual composes proxy-groups and rules from node groups plus the
	// routing model, overwriting whatever the template carries.
	ModeVisual = "visual"
	// ModeYAML leaves the template in charge, tokens and all.
	ModeYAML = "yaml"
)

// Mode returns the configured rendering mode, defaulting to visual: a fresh
// install should land in the editor an admin can actually use.
func Mode(db *store.DB) string {
	if db.GetSetting(SettingKeyMode, ModeVisual) == ModeYAML {
		return ModeYAML
	}
	return ModeVisual
}

// Routing returns the stored routing model and whether it was customised. A
// value that fails to parse falls back to the default rather than failing the
// subscription — a client mid-refresh should not go dark over a bad setting.
func Routing(db *store.DB) (*clash.Routing, bool) {
	raw := db.GetSetting(SettingKeyRouting, "")
	if raw == "" {
		return clash.DefaultRouting(), false
	}
	var r clash.Routing
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return clash.DefaultRouting(), false
	}
	return &r, true
}

// NodeGroups returns the enabled groups in render order together with their
// member node ids.
func NodeGroups(db *store.DB) ([]model.NodeGroup, map[uint][]uint) {
	var groups []model.NodeGroup
	db.Where("enabled = ?", true).Order("sort_order ASC, id ASC").Find(&groups)
	if len(groups) == 0 {
		return nil, nil
	}
	ids := make([]uint, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	var links []model.NodeGroupNode
	db.Where("group_id IN ?", ids).Find(&links)
	members := make(map[uint][]uint, len(groups))
	for _, link := range links {
		members[link.GroupID] = append(members[link.GroupID], link.NodeID)
	}
	return groups, members
}

// GroupsFor narrows each node group to the proxies this user actually has.
// A group whose members are all outside the user's plan comes back empty and
// is pruned during rendering.
//
// With no groups defined at all, groups are derived from the nodes' regions
// instead. That keeps a fresh install grouping the way it always did, and the
// groups page offers to materialise the same split as real rows.
func GroupsFor(db *store.DB, entries []Entry) []clash.Group {
	groups, members := NodeGroups(db)
	if len(groups) == 0 {
		return regionGroups(entries)
	}
	// Entry order is the node display order, so members inherit it.
	nameByNode := make(map[uint]string, len(entries))
	order := make([]uint, 0, len(entries))
	for _, e := range entries {
		if e.Node.ID == 0 || e.Clash == nil {
			continue
		}
		if _, seen := nameByNode[e.Node.ID]; !seen {
			order = append(order, e.Node.ID)
		}
		nameByNode[e.Node.ID] = e.Name
	}

	out := make([]clash.Group, 0, len(groups))
	for _, g := range groups {
		wanted := map[uint]bool{}
		for _, id := range members[g.ID] {
			wanted[id] = true
		}
		names := make([]string, 0, len(wanted))
		for _, id := range order {
			if wanted[id] {
				names = append(names, nameByNode[id])
			}
		}
		out = append(out, clash.Group{
			Name:      g.Name,
			Display:   g.DisplayName(),
			Type:      g.Type,
			TestURL:   g.TestURL,
			Interval:  g.Interval,
			Tolerance: g.Tolerance,
			Members:   names,
		})
	}
	return out
}

// regionGroups is the bootstrap grouping: one url-test group per region, in
// the order the regions first appear, named after the region text itself.
func regionGroups(entries []Entry) []clash.Group {
	var order []string
	byRegion := map[string][]string{}
	for _, e := range entries {
		region := e.Node.Region
		if region == "" || e.Clash == nil {
			continue
		}
		if _, seen := byRegion[region]; !seen {
			order = append(order, region)
		}
		byRegion[region] = append(byRegion[region], e.Name)
	}
	out := make([]clash.Group, 0, len(order))
	for _, region := range order {
		out = append(out, clash.Group{
			Name:    region,
			Display: region,
			Type:    model.GroupTypeURLTest,
			Members: byRegion[region],
		})
	}
	return out
}

// RegionSplit reports how a bootstrap "generate groups from regions" would
// divide the given nodes, so the UI can preview it before creating rows.
func RegionSplit(nodes []model.Node) ([]string, map[string][]uint) {
	var order []string
	members := map[string][]uint{}
	for _, n := range nodes {
		if n.Region == "" {
			continue
		}
		if _, seen := members[n.Region]; !seen {
			order = append(order, n.Region)
		}
		members[n.Region] = append(members[n.Region], n.ID)
	}
	return order, members
}
