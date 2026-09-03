// Package routing turns FireX's stored routing matrix into one user's config.
//
// Two questions are answered here, and they are deliberately independent:
// which inbounds a user may reach (their profile's node-group whitelist, and
// nothing else), and how their traffic is split across those inbounds (the
// policies, and the egress each one takes for that profile). Editing rules or
// egresses therefore never changes what has to be pushed to a panel.
package routing

import (
	"sort"
	"strings"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// Proxy is one rendered proxy together with the inbound it came from, so node
// groups can be narrowed to the proxies this particular user actually holds.
type Proxy struct {
	InboundID uint
	Name      string
	Entry     *clash.Ordered
}

// Groups returns the node groups a profile grants, in render order. A profile
// that whitelists everything follows the node-group order itself.
func Groups(db *store.DB, profileID uint) ([]model.NodeGroup, error) {
	if profileID == 0 {
		return nil, nil
	}
	var profile model.Profile
	if err := db.First(&profile, profileID).Error; err != nil {
		if store.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if !profile.Enabled {
		return nil, nil
	}
	q := db.Where("enabled = ?", true).Order("sort_order ASC, id ASC")
	if !profile.AllGroups {
		q = q.Where("id IN (?)", db.Model(&model.ProfileNodeGroup{}).
			Select("group_id").Where("profile_id = ?", profileID))
	}
	var groups []model.NodeGroup
	err := q.Find(&groups).Error
	return groups, err
}

// InboundsForProfile is the profile's whitelist flattened to the inbounds that
// can actually carry traffic right now, in display order and without repeats.
func InboundsForProfile(db *store.DB, profileID uint) ([]model.Inbound, error) {
	groups, err := Groups(db, profileID)
	if err != nil || len(groups) == 0 {
		return nil, err
	}
	ids := make([]uint, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	var inbounds []model.Inbound
	err = db.
		Distinct("inbounds.*").
		Joins("JOIN node_group_inbounds m ON m.inbound_id = inbounds.id").
		Joins("JOIN panels ON panels.id = inbounds.panel_id").
		Where("m.group_id IN ?", ids).
		Where("inbounds.enabled = ? AND inbounds.missing = ?", true, false).
		Where("panels.enabled = ?", true).
		Order("inbounds.sort_order ASC, inbounds.id ASC").
		Find(&inbounds).Error
	return inbounds, err
}

// membership maps each node group to the inbounds in it, restricted to ids the
// caller cares about.
func membership(db *store.DB, groupIDs []uint) (map[uint][]uint, error) {
	if len(groupIDs) == 0 {
		return map[uint][]uint{}, nil
	}
	var links []model.NodeGroupInbound
	if err := db.Where("group_id IN ?", groupIDs).Find(&links).Error; err != nil {
		return nil, err
	}
	out := make(map[uint][]uint, len(groupIDs))
	for _, link := range links {
		out[link.GroupID] = append(out[link.GroupID], link.InboundID)
	}
	return out, nil
}

// cell is one policy resolved for one profile: the egress that applies plus the
// rules the policy owns.
type cell struct {
	policy  model.Policy
	egress  model.Egress
	members []model.EgressMember
	rules   []model.Rule
}

// Compile renders the whole matrix down to one profile's proxy-groups and rule
// lines. References that no longer resolve — a node group the profile does not
// grant, a policy that is hidden here — are dropped rather than emitted, and
// any group left empty is pruned by clash.Render.
func Compile(db *store.DB, profileID uint, proxies []Proxy) (clash.Input, error) {
	in := clash.Input{Proxies: make([]clash.Proxy, 0, len(proxies))}
	proxyNames := make([]string, 0, len(proxies))
	nameByInbound := make(map[uint]string, len(proxies))
	inboundOrder := make([]uint, 0, len(proxies))
	for _, p := range proxies {
		if p.Entry == nil {
			continue
		}
		in.Proxies = append(in.Proxies, clash.Proxy{Name: p.Name, Entry: p.Entry})
		proxyNames = append(proxyNames, p.Name)
		if p.InboundID != 0 {
			if _, seen := nameByInbound[p.InboundID]; !seen {
				inboundOrder = append(inboundOrder, p.InboundID)
				nameByInbound[p.InboundID] = p.Name
			}
		}
	}

	groups, err := Groups(db, profileID)
	if err != nil {
		return in, err
	}
	groupIDs := make([]uint, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.ID)
	}
	members, err := membership(db, groupIDs)
	if err != nil {
		return in, err
	}

	// Node group members inherit the proxy display order, so a client's list
	// reads the same way everywhere.
	groupMembers := make(map[string][]string, len(groups))
	groupDisplay := make(map[string]string, len(groups))
	groupOrder := make([]string, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		wanted := make(map[uint]bool, len(members[g.ID]))
		for _, id := range members[g.ID] {
			wanted[id] = true
		}
		names := make([]string, 0, len(wanted))
		for _, id := range inboundOrder {
			if wanted[id] {
				names = append(names, nameByInbound[id])
			}
		}
		groupMembers[g.Name] = names
		groupDisplay[g.Name] = g.DisplayName()
		groupOrder = append(groupOrder, g.Name)
	}

	cells, err := loadCells(db, profileID)
	if err != nil {
		return in, err
	}
	visible := make(map[string]model.Policy, len(cells))
	for _, c := range cells {
		visible[c.policy.Name] = c.policy
	}

	resolve := func(m model.EgressMember) []string {
		switch m.Kind {
		case model.MemberNodeGroup:
			if display, ok := groupDisplay[m.Ref]; ok {
				return []string{display}
			}
		case model.MemberPolicy:
			if policy, ok := visible[m.Ref]; ok {
				return []string{policy.DisplayName()}
			}
		case model.MemberBuiltin:
			return []string{m.Ref}
		case model.MemberAllNodeGroups:
			out := make([]string, 0, len(groupOrder))
			for _, name := range groupOrder {
				out = append(out, groupDisplay[name])
			}
			return out
		case model.MemberAllInbounds:
			return proxyNames
		}
		return nil
	}

	// Policies lead: they are what a client's group list opens with. The node
	// groups they draw from follow in their configured order.
	in.Groups = make([]clash.Group, 0, len(cells)+len(groups))
	for _, c := range cells {
		names := make([]string, 0, len(c.members))
		seen := map[string]bool{}
		for _, m := range c.members {
			for _, name := range resolve(m) {
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
		in.Groups = append(in.Groups, clash.Group{
			Name:      c.policy.DisplayName(),
			Type:      c.egress.Type,
			TestURL:   c.egress.TestURL,
			Interval:  c.egress.Interval,
			Tolerance: c.egress.Tolerance,
			Members:   names,
		})
	}
	for i := range groups {
		g := &groups[i]
		in.Groups = append(in.Groups, clash.Group{
			Name:      g.DisplayName(),
			Type:      g.Type,
			TestURL:   g.TestURL,
			Interval:  g.Interval,
			Tolerance: g.Tolerance,
			Members:   groupMembers[g.Name],
		})
	}

	in.Rules = compileRules(cells)
	return in, nil
}

// compileRules walks the policies in order, emitting each one's rules against
// its own display name, and closes with the MATCH line the final policy owns.
func compileRules(cells []cell) []string {
	out := make([]string, 0, 16)
	final := ""
	for _, c := range cells {
		display := c.policy.DisplayName()
		if c.policy.IsFinal {
			final = display
		}
		for _, rule := range c.rules {
			if rule.Disabled {
				continue
			}
			value := strings.TrimSpace(rule.Value)
			if value == "" {
				continue
			}
			line := rule.Type + "," + value + "," + display
			if rule.NoResolve && NoResolveTypes[rule.Type] {
				line += ",no-resolve"
			}
			out = append(out, line)
		}
	}
	if final == "" {
		final = "DIRECT"
	}
	return append(out, "MATCH,"+final)
}

// loadCells returns the policies this profile can see, in order, each paired
// with the egress that applies: its own override when there is one, otherwise
// the default column. A hidden cell drops the policy entirely.
func loadCells(db *store.DB, profileID uint) ([]cell, error) {
	var policies []model.Policy
	if err := db.Where("enabled = ?", true).Order("sort_order ASC, id ASC").Find(&policies).Error; err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return nil, nil
	}
	policyIDs := make([]uint, 0, len(policies))
	for _, p := range policies {
		policyIDs = append(policyIDs, p.ID)
	}

	var egresses []model.Egress
	err := db.Where("policy_id IN ?", policyIDs).
		Where("profile_id IN ?", []uint{model.DefaultProfileID, profileID}).
		Find(&egresses).Error
	if err != nil {
		return nil, err
	}
	byCell := make(map[[2]uint]model.Egress, len(egresses))
	egressIDs := make([]uint, 0, len(egresses))
	for _, e := range egresses {
		byCell[[2]uint{e.PolicyID, e.ProfileID}] = e
		egressIDs = append(egressIDs, e.ID)
	}

	var memberRows []model.EgressMember
	if len(egressIDs) > 0 {
		if err := db.Where("egress_id IN ?", egressIDs).Order("sort_order ASC, id ASC").Find(&memberRows).Error; err != nil {
			return nil, err
		}
	}
	membersByEgress := map[uint][]model.EgressMember{}
	for _, m := range memberRows {
		membersByEgress[m.EgressID] = append(membersByEgress[m.EgressID], m)
	}

	var ruleRows []model.Rule
	if err := db.Where("policy_id IN ?", policyIDs).Order("sort_order ASC, id ASC").Find(&ruleRows).Error; err != nil {
		return nil, err
	}
	rulesByPolicy := map[uint][]model.Rule{}
	for _, r := range ruleRows {
		rulesByPolicy[r.PolicyID] = append(rulesByPolicy[r.PolicyID], r)
	}

	out := make([]cell, 0, len(policies))
	for _, policy := range policies {
		egress, ok := byCell[[2]uint{policy.ID, profileID}]
		if !ok {
			egress, ok = byCell[[2]uint{policy.ID, model.DefaultProfileID}]
		}
		if !ok || egress.Hidden {
			continue
		}
		out = append(out, cell{
			policy:  policy,
			egress:  egress,
			members: membersByEgress[egress.ID],
			rules:   rulesByPolicy[policy.ID],
		})
	}
	// The MATCH target has to be the last rule mihomo sees, whatever sort order
	// an admin gave it.
	sort.SliceStable(out, func(i, j int) bool { return !out[i].policy.IsFinal && out[j].policy.IsFinal })
	return out, nil
}

// ------------------------------------------------------------ reverse lookups

// PlansUsingProfile lists the plans bound to a profile, so a whitelist edit can
// be pushed to exactly the users it affects.
func PlansUsingProfile(db *store.DB, profileID uint) []uint {
	var ids []uint
	if profileID == 0 {
		return ids
	}
	db.Model(&model.Plan{}).Where("profile_id = ?", profileID).Pluck("id", &ids)
	return ids
}

// PlansUsingNodeGroup lists the plans that reach a node group, through either an
// explicit whitelist or a profile that takes every group.
func PlansUsingNodeGroup(db *store.DB, groupID uint) []uint {
	var profileIDs []uint
	db.Model(&model.ProfileNodeGroup{}).Where("group_id = ?", groupID).Pluck("profile_id", &profileIDs)
	var wildcard []uint
	db.Model(&model.Profile{}).Where("all_groups = ?", true).Pluck("id", &wildcard)
	profileIDs = append(profileIDs, wildcard...)
	if len(profileIDs) == 0 {
		return nil
	}
	var ids []uint
	db.Model(&model.Plan{}).Where("profile_id IN ?", profileIDs).Pluck("id", &ids)
	return ids
}

// PlansUsingInbound lists the plans that reach an inbound through any group.
func PlansUsingInbound(db *store.DB, inboundID uint) []uint {
	var groupIDs []uint
	db.Model(&model.NodeGroupInbound{}).Where("inbound_id = ?", inboundID).Pluck("group_id", &groupIDs)
	seen := map[uint]bool{}
	var out []uint
	for _, groupID := range groupIDs {
		for _, planID := range PlansUsingNodeGroup(db, groupID) {
			if !seen[planID] {
				seen[planID] = true
				out = append(out, planID)
			}
		}
	}
	return out
}
