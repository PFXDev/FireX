package routing

import (
	"sort"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// PanelShare is one panel's effective part of a node group's traffic.
type PanelShare struct {
	PanelID uint `json:"panelId"`
	// Weight is in percent; a group's weights add up to 100.
	Weight int `json:"weight"`
}

// GroupPanels maps each node group to the panels hosting its enabled, present
// inbounds, in id order. Those are the panels a group's quota is split over.
func GroupPanels(db *store.DB, groupIDs []uint) (map[uint][]uint, error) {
	out := map[uint][]uint{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		GroupID uint
		PanelID uint
	}
	err := db.Table("node_group_inbounds AS m").
		Distinct("m.group_id", "inbounds.panel_id").
		Joins("JOIN inbounds ON inbounds.id = m.inbound_id").
		Where("m.group_id IN ?", groupIDs).
		Where("inbounds.enabled = ? AND inbounds.missing = ?", true, false).
		Order("inbounds.panel_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.GroupID] = append(out[r.GroupID], r.PanelID)
	}
	return out, nil
}

// GroupShares is each group's split over the panels it spans. The stored
// weights apply when they cover every one of those panels; otherwise, as for
// a group never split by hand, every panel gets the same part.
func GroupShares(db *store.DB, groupIDs []uint) (map[uint][]PanelShare, error) {
	panels, err := GroupPanels(db, groupIDs)
	if err != nil {
		return nil, err
	}
	var stored []model.NodeGroupPanelShare
	if len(groupIDs) > 0 {
		if err := db.Where("group_id IN ?", groupIDs).Find(&stored).Error; err != nil {
			return nil, err
		}
	}
	weights := map[uint]map[uint]int{}
	for _, s := range stored {
		if weights[s.GroupID] == nil {
			weights[s.GroupID] = map[uint]int{}
		}
		weights[s.GroupID][s.PanelID] = s.Weight
	}
	out := make(map[uint][]PanelShare, len(panels))
	for groupID, panelIDs := range panels {
		out[groupID] = splitShares(panelIDs, weights[groupID])
	}
	return out, nil
}

// splitShares turns stored weights into percentages that add up to 100,
// rounding by largest remainder so the parts never drift off the whole.
func splitShares(panelIDs []uint, stored map[uint]int) []PanelShare {
	raw := make([]int, len(panelIDs))
	total := 0
	for i, id := range panelIDs {
		w, ok := stored[id]
		if !ok || w <= 0 {
			total = 0
			break
		}
		raw[i] = w
		total += w
	}
	if total == 0 {
		for i := range raw {
			raw[i] = 1
		}
		total = len(raw)
	}
	out := make([]PanelShare, len(panelIDs))
	remainders := make([]int, len(panelIDs))
	sum := 0
	for i, id := range panelIDs {
		out[i] = PanelShare{PanelID: id, Weight: raw[i] * 100 / total}
		remainders[i] = raw[i] * 100 % total
		sum += out[i].Weight
	}
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return remainders[order[a]] > remainders[order[b]] })
	for i := 0; sum < 100 && i < len(order); i++ {
		out[order[i]].Weight++
		sum++
	}
	return out
}

// PlanGroupPanelCaps turns a plan's node-group quotas into a per-user cap on
// each panel: the group's quota times the panel's share of it. Only groups
// the plan's profile grants count. A panel is not expected to serve two
// groups; if one does, its caps from both add up.
func PlanGroupPanelCaps(db *store.DB, plan *model.Plan) (map[uint]int64, error) {
	out := map[uint]int64{}
	if plan == nil {
		return out, nil
	}
	var limits []model.PlanGroupLimit
	if err := db.Where("plan_id = ? AND traffic_bytes > 0", plan.ID).Find(&limits).Error; err != nil {
		return nil, err
	}
	if len(limits) == 0 {
		return out, nil
	}
	groups, err := Groups(db, plan.ProfileID)
	if err != nil {
		return nil, err
	}
	granted := map[uint]bool{}
	ids := make([]uint, 0, len(groups))
	for _, g := range groups {
		granted[g.ID] = true
		ids = append(ids, g.ID)
	}
	shares, err := GroupShares(db, ids)
	if err != nil {
		return nil, err
	}
	for _, l := range limits {
		if !granted[l.GroupID] {
			continue
		}
		for _, s := range shares[l.GroupID] {
			// Every share is at least 1%, so a capped panel never rounds
			// down to 0, which would read as unlimited.
			out[s.PanelID] += max(1, l.TrafficBytes*int64(s.Weight)/100)
		}
	}
	return out, nil
}
