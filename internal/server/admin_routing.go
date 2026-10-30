package server

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
)

// tx wraps a transaction handle so package routing can validate the matrix in
// the same shape it will later render from.
func tx(handle *gorm.DB) *store.DB { return &store.DB{DB: handle} }

// -------------------------------------------------------------- node groups

type nodeGroupTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type nodeGroupRow struct {
	model.NodeGroup
	Tags       []nodeGroupTag `json:"tags"`
	InboundIDs []uint         `json:"inboundIds"`
	// UsableInbounds counts the members a subscription would actually render, so
	// a group that looks populated but ships nothing is visible in the list.
	UsableInbounds int `json:"usableInbounds"`
	// ProfileCount is how many profiles grant this group. Zero means no user can
	// reach it.
	ProfileCount int `json:"profileCount"`
	// PanelShares is how a node-group quota splits over the group's panels,
	// as enforced: stored weights, or an even split when none cover them all.
	PanelShares []routing.PanelShare `json:"panelShares"`
}

func (s *Server) listNodeGroups(c *gin.Context) {
	var groups []model.NodeGroup
	if err := s.db.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	var links []model.NodeGroupInbound
	s.db.Find(&links)
	members := map[uint][]uint{}
	for _, link := range links {
		members[link.GroupID] = append(members[link.GroupID], link.InboundID)
	}

	var tags []model.NodeGroupTag
	s.db.Order("key ASC").Find(&tags)
	tagsByGroup := map[uint][]nodeGroupTag{}
	for _, t := range tags {
		tagsByGroup[t.GroupID] = append(tagsByGroup[t.GroupID], nodeGroupTag{Key: t.Key, Value: t.Value})
	}

	var inbounds []model.Inbound
	s.db.Find(&inbounds)
	usable := map[uint]bool{}
	for _, n := range inbounds {
		usable[n.ID] = n.Enabled && !n.Missing
	}

	var wildcards int64
	s.db.Model(&model.Profile{}).Where("all_groups = ?", true).Count(&wildcards)
	var grants []model.ProfileNodeGroup
	s.db.Find(&grants)
	profileCount := map[uint]int{}
	for _, g := range grants {
		profileCount[g.GroupID]++
	}

	groupIDs := make([]uint, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.ID)
	}
	shares, err := routing.GroupShares(s.db, groupIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	out := make([]nodeGroupRow, 0, len(groups))
	for _, g := range groups {
		row := nodeGroupRow{
			NodeGroup:    g,
			Tags:         jsonList(tagsByGroup[g.ID]),
			InboundIDs:   jsonList(members[g.ID]),
			ProfileCount: profileCount[g.ID] + int(wildcards),
			PanelShares:  jsonList(shares[g.ID]),
		}
		for _, id := range row.InboundIDs {
			if usable[id] {
				row.UsableInbounds++
			}
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

type nodeGroupRequest struct {
	Name       string         `json:"name"`
	Emoji      string         `json:"emoji"`
	Type       string         `json:"type"`
	TestURL    string         `json:"testUrl"`
	Interval   *int           `json:"interval"`
	Tolerance  *int           `json:"tolerance"`
	Multiplier *float64       `json:"multiplier"`
	SortOrder  *int           `json:"sortOrder"`
	Enabled    *bool          `json:"enabled"`
	Remark     string         `json:"remark"`
	Tags       []nodeGroupTag `json:"tags"`
	InboundIDs []uint         `json:"inboundIds"`
	// PanelShares replaces the group's per-panel weights when present; nil
	// keeps them.
	PanelShares []routing.PanelShare `json:"panelShares"`
}

// validShares rejects weights outside 0..100; zero drops the panel's weight.
func validShares(shares []routing.PanelShare) error {
	for _, sh := range shares {
		if sh.Weight < 0 || sh.Weight > 100 {
			return errBadRequest("面板比例必须在 0 到 100 之间")
		}
	}
	return nil
}

// apply copies the request onto a group, rejecting the shapes that would
// produce a config mihomo cannot load.
func (req *nodeGroupRequest) apply(g *model.NodeGroup) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errBadRequest("节点组需要一个名称")
	}
	// Rules are comma-separated lines; a comma anywhere in a rendered group
	// name splits them into nonsense.
	if strings.Contains(name, ",") || strings.Contains(req.Emoji, ",") {
		return errBadRequest("名称和图标不能包含英文逗号")
	}
	// The rendered name is what a rule targets; one that reads DIRECT would
	// shadow mihomo's own.
	if probe := (model.NodeGroup{Name: name, Emoji: strings.TrimSpace(req.Emoji)}); routing.IsReserved(probe.DisplayName()) {
		return errBadRequest("「" + probe.DisplayName() + "」是 mihomo 的内置策略名，换一个")
	}
	groupType := strings.TrimSpace(req.Type)
	if groupType == "" {
		groupType = model.GroupTypeURLTest
	}
	if !knownGroupType(groupType) {
		return errBadRequest("选择方式必须是 select、url-test、fallback、load-balance 之一")
	}
	g.Name = name
	g.Emoji = strings.TrimSpace(req.Emoji)
	g.Type = groupType
	g.TestURL = strings.TrimSpace(req.TestURL)
	g.Remark = req.Remark
	if req.Interval != nil {
		g.Interval = *req.Interval
	}
	if req.Tolerance != nil {
		g.Tolerance = *req.Tolerance
	}
	if req.Multiplier != nil {
		g.Multiplier = *req.Multiplier
	}
	if req.SortOrder != nil {
		g.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		g.Enabled = *req.Enabled
	}
	return nil
}

func knownGroupType(t string) bool {
	for _, known := range clash.GroupTypes {
		if known == t {
			return true
		}
	}
	return false
}

func (s *Server) createNodeGroup(c *gin.Context) {
	var req nodeGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	g := model.NodeGroup{
		Type: model.GroupTypeURLTest, Interval: 300, Tolerance: 50,
		Multiplier: 1, SortOrder: 100, Enabled: true,
	}
	if err := req.apply(&g); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := validShares(req.PanelShares); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Create(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := setGroupTags(s.db.DB, g.ID, req.Tags); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if _, err := setGroupMembers(s.db.DB, g.ID, req.InboundIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if _, err := setGroupShares(s.db.DB, g.ID, req.PanelShares); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// A brand-new group is reachable straight away by every profile that takes
	// all groups, so its inbounds have to be pushed.
	s.subs.InvalidateAll()
	syncErr := s.reconcilePlans(routing.PlansUsingNodeGroup(s.db, g.ID))
	c.JSON(http.StatusOK, gin.H{"group": g, "syncError": errString(syncErr)})
}

func (s *Server) updateNodeGroup(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req nodeGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var g model.NodeGroup
	if err := s.db.First(&g, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "node group not found")
		return
	}
	previousName := g.Name
	wasEnabled := g.Enabled
	if err := req.apply(&g); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := validShares(req.PanelShares); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Save(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := setGroupTags(s.db.DB, g.ID, req.Tags); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	membersChanged, err := setGroupMembers(s.db.DB, g.ID, req.InboundIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	sharesChanged := false
	if req.PanelShares != nil {
		if sharesChanged, err = setGroupShares(s.db.DB, g.ID, req.PanelShares); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
	}
	// Egress members reference a group by its bare name, so a rename has to
	// travel with it or every egress pointing here would dangle.
	rewritten := int64(0)
	if previousName != g.Name {
		res := s.db.Model(&model.EgressMember{}).
			Where("kind = ? AND ref = ?", model.MemberNodeGroup, previousName).
			Update("ref", g.Name)
		rewritten = res.RowsAffected
	}
	s.subs.InvalidateAll()
	// Only membership and the enabled flag change which inbounds a user holds,
	// and the shares the per-panel caps a node-group quota yields; a relabel
	// must not re-push the whole fleet.
	var syncErr error
	if membersChanged || sharesChanged || wasEnabled != g.Enabled {
		syncErr = s.reconcilePlans(routing.PlansUsingNodeGroup(s.db, g.ID))
	}
	c.JSON(http.StatusOK, gin.H{"group": g, "rewrittenMembers": rewritten, "syncError": errString(syncErr)})
}

func (s *Server) deleteNodeGroup(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var g model.NodeGroup
	if err := s.db.First(&g, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "node group not found")
		return
	}
	// Capture the affected plans before the rows that link them are gone.
	plans := routing.PlansUsingNodeGroup(s.db, g.ID)

	dropped, err := deleteNodeGroupRows(s.db.DB, g)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	syncErr := s.reconcilePlans(plans)
	c.JSON(http.StatusOK, gin.H{"ok": true, "droppedMembers": dropped, "syncError": errString(syncErr)})
}

// deleteNodeGroupRows removes a group with its membership, tags and grants,
// and drops the egress members that named it. It returns how many it dropped.
func deleteNodeGroupRows(handle *gorm.DB, g model.NodeGroup) (int64, error) {
	handle.Where("group_id = ?", g.ID).Delete(&model.NodeGroupInbound{})
	handle.Where("group_id = ?", g.ID).Delete(&model.NodeGroupTag{})
	handle.Where("group_id = ?", g.ID).Delete(&model.NodeGroupPanelShare{})
	handle.Where("group_id = ?", g.ID).Delete(&model.PlanGroupLimit{})
	handle.Where("group_id = ?", g.ID).Delete(&model.ProfileNodeGroup{})
	dropped := handle.Where("kind = ? AND ref = ?", model.MemberNodeGroup, g.Name).
		Delete(&model.EgressMember{}).RowsAffected
	return dropped, handle.Delete(&model.NodeGroup{}, g.ID).Error
}

// setGroupMembers replaces a group's membership wholesale; the editor always
// sends the complete list. It reports whether the set actually changed, so the
// caller can skip a fleet-wide reconcile for a relabel.
func setGroupMembers(handle *gorm.DB, groupID uint, inboundIDs []uint) (bool, error) {
	var before []uint
	handle.Model(&model.NodeGroupInbound{}).Where("group_id = ?", groupID).Order("inbound_id ASC").Pluck("inbound_id", &before)

	if err := handle.Where("group_id = ?", groupID).Delete(&model.NodeGroupInbound{}).Error; err != nil {
		return false, err
	}
	var existing []uint
	if len(inboundIDs) > 0 {
		handle.Model(&model.Inbound{}).Where("id IN ?", inboundIDs).Pluck("id", &existing)
	}
	valid := map[uint]bool{}
	for _, id := range existing {
		valid[id] = true
	}
	links := make([]model.NodeGroupInbound, 0, len(inboundIDs))
	seen := map[uint]bool{}
	after := make([]uint, 0, len(inboundIDs))
	for _, id := range inboundIDs {
		if !valid[id] || seen[id] {
			continue
		}
		seen[id] = true
		links = append(links, model.NodeGroupInbound{GroupID: groupID, InboundID: id})
		after = append(after, id)
	}
	sort.Slice(after, func(i, j int) bool { return after[i] < after[j] })
	changed := !slices.Equal(before, after)
	if len(links) == 0 {
		return changed, nil
	}
	return changed, handle.Create(&links).Error
}

// setGroupShares replaces a group's per-panel weights, keeping only panels that
// exist and carry a weight. It reports whether the stored weights changed.
func setGroupShares(handle *gorm.DB, groupID uint, shares []routing.PanelShare) (bool, error) {
	var before []model.NodeGroupPanelShare
	handle.Where("group_id = ?", groupID).Order("panel_id ASC").Find(&before)
	if err := handle.Where("group_id = ?", groupID).Delete(&model.NodeGroupPanelShare{}).Error; err != nil {
		return false, err
	}
	var existing []uint
	ids := make([]uint, 0, len(shares))
	for _, sh := range shares {
		ids = append(ids, sh.PanelID)
	}
	if len(ids) > 0 {
		handle.Model(&model.Panel{}).Where("id IN ?", ids).Pluck("id", &existing)
	}
	valid := map[uint]bool{}
	for _, id := range existing {
		valid[id] = true
	}
	rows := make([]model.NodeGroupPanelShare, 0, len(shares))
	seen := map[uint]bool{}
	for _, sh := range shares {
		if !valid[sh.PanelID] || seen[sh.PanelID] || sh.Weight <= 0 {
			continue
		}
		seen[sh.PanelID] = true
		rows = append(rows, model.NodeGroupPanelShare{GroupID: groupID, PanelID: sh.PanelID, Weight: sh.Weight})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PanelID < rows[j].PanelID })
	changed := !slices.Equal(before, rows)
	if len(rows) == 0 {
		return changed, nil
	}
	return changed, handle.Create(&rows).Error
}

func setGroupTags(handle *gorm.DB, groupID uint, tags []nodeGroupTag) error {
	if err := handle.Where("group_id = ?", groupID).Delete(&model.NodeGroupTag{}).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		key := strings.TrimSpace(tag.Key)
		value := strings.TrimSpace(tag.Value)
		if key == "" || value == "" || seen[key] {
			continue
		}
		seen[key] = true
		if err := handle.Create(&model.NodeGroupTag{GroupID: groupID, Key: key, Value: value}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ----------------------------------------------------------------- profiles

type profileRow struct {
	model.Profile
	GroupIDs  []uint `json:"groupIds"`
	PlanCount int64  `json:"planCount"`
	// UsableInbounds is how many inbounds this profile would actually hand out.
	UsableInbounds int `json:"usableInbounds"`
}

func (s *Server) listProfiles(c *gin.Context) {
	var profiles []model.Profile
	if err := s.db.Order("sort_order ASC, id ASC").Find(&profiles).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var grants []model.ProfileNodeGroup
	s.db.Find(&grants)
	byProfile := map[uint][]uint{}
	for _, g := range grants {
		byProfile[g.ProfileID] = append(byProfile[g.ProfileID], g.GroupID)
	}

	out := make([]profileRow, 0, len(profiles))
	for _, p := range profiles {
		row := profileRow{Profile: p, GroupIDs: jsonList(byProfile[p.ID])}
		s.db.Model(&model.Plan{}).Where("profile_id = ?", p.ID).Count(&row.PlanCount)
		inbounds, _ := routing.InboundsForProfile(s.db, p.ID)
		row.UsableInbounds = len(inbounds)
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

type profileRequest struct {
	Name      string `json:"name"`
	AllGroups *bool  `json:"allGroups"`
	SortOrder *int   `json:"sortOrder"`
	Enabled   *bool  `json:"enabled"`
	Remark    string `json:"remark"`
	GroupIDs  []uint `json:"groupIds"`
}

func (req *profileRequest) apply(p *model.Profile) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errBadRequest("分流方案需要一个名称")
	}
	p.Name = name
	p.Remark = req.Remark
	if req.AllGroups != nil {
		p.AllGroups = *req.AllGroups
	}
	if req.SortOrder != nil {
		p.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	return nil
}

func (s *Server) createProfile(c *gin.Context) {
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	p := model.Profile{SortOrder: 100, Enabled: true}
	if err := req.apply(&p); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Create(&p).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := setProfileGroups(s.db.DB, p.ID, req.GroupIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *Server) updateProfile(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var p model.Profile
	if err := s.db.First(&p, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "profile not found")
		return
	}
	if err := req.apply(&p); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Save(&p).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := setProfileGroups(s.db.DB, p.ID, req.GroupIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// The whitelist is the only thing that decides what gets pushed to a panel,
	// so this is one of the few edits that must reach the fleet before we answer.
	s.subs.InvalidateAll()
	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ReconcileUsersOfPlans(ctx, routing.PlansUsingProfile(s.db, p.ID))
	c.JSON(http.StatusOK, gin.H{"profile": p, "syncError": errString(syncErr)})
}

func (s *Server) deleteProfile(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var count int64
	s.db.Model(&model.Plan{}).Where("profile_id = ?", id).Count(&count)
	if count > 0 {
		failMsg(c, http.StatusConflict, "还有套餐绑定这个分流方案，先改掉它们")
		return
	}
	if err := deleteProfileRows(s.db.DB, id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// deleteProfileRows removes a profile with its whitelist. The profile's column
// of overrides goes with it; the default column stays. Callers check first
// that no plan binds it.
func deleteProfileRows(handle *gorm.DB, id uint) error {
	handle.Where("profile_id = ?", id).Delete(&model.ProfileNodeGroup{})
	var egressIDs []uint
	handle.Model(&model.Egress{}).Where("profile_id = ?", id).Pluck("id", &egressIDs)
	if len(egressIDs) > 0 {
		handle.Where("egress_id IN ?", egressIDs).Delete(&model.EgressMember{})
		handle.Where("id IN ?", egressIDs).Delete(&model.Egress{})
	}
	return handle.Delete(&model.Profile{}, id).Error
}

func setProfileGroups(handle *gorm.DB, profileID uint, groupIDs []uint) error {
	if err := handle.Where("profile_id = ?", profileID).Delete(&model.ProfileNodeGroup{}).Error; err != nil {
		return err
	}
	if len(groupIDs) == 0 {
		return nil
	}
	var existing []uint
	handle.Model(&model.NodeGroup{}).Where("id IN ?", groupIDs).Pluck("id", &existing)
	valid := map[uint]bool{}
	for _, id := range existing {
		valid[id] = true
	}
	rows := make([]model.ProfileNodeGroup, 0, len(groupIDs))
	seen := map[uint]bool{}
	for _, id := range groupIDs {
		if !valid[id] || seen[id] {
			continue
		}
		seen[id] = true
		rows = append(rows, model.ProfileNodeGroup{ProfileID: profileID, GroupID: id})
	}
	if len(rows) == 0 {
		return nil
	}
	return handle.Create(&rows).Error
}

// ------------------------------------------------------------------- matrix

type matrixRule struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	NoResolve bool   `json:"noResolve"`
	Disabled  bool   `json:"disabled"`
}

type matrixMember struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type matrixPolicy struct {
	ID      uint         `json:"id"`
	Name    string       `json:"name"`
	Icon    string       `json:"icon"`
	IsFinal bool         `json:"isFinal"`
	Enabled bool         `json:"enabled"`
	Remark  string       `json:"remark"`
	Rules   []matrixRule `json:"rules"`
}

type matrixEgress struct {
	// PolicyIndex addresses a row by position rather than id, so cells for a
	// policy created in this same save need no id to exist yet.
	PolicyIndex int `json:"policyIndex"`
	// ProfileID is 0 for the default column.
	ProfileID    uint           `json:"profileId"`
	Type         string         `json:"type"`
	TestURL      string         `json:"testUrl"`
	Interval     int            `json:"interval"`
	Tolerance    int            `json:"tolerance"`
	Hidden       bool           `json:"hidden"`
	ClientHidden bool           `json:"clientHidden"`
	Members      []matrixMember `json:"members"`
}

type matrixRuleSet struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Payload string `json:"payload"`
	Remark  string `json:"remark"`
}

type matrixDoc struct {
	// Policies are in row order: it is both the rule precedence and the order
	// the groups appear in a client.
	Policies []matrixPolicy `json:"policies"`
	Egresses []matrixEgress `json:"egresses"`
	// RuleSets travel with the matrix because RULE-SET rules name them. A
	// document without the key leaves the stored sets alone.
	RuleSets []matrixRuleSet `json:"ruleSets"`
}

func (s *Server) getRouting(c *gin.Context) {
	var policies []model.Policy
	s.db.Order("sort_order ASC, id ASC").Find(&policies)
	var rules []model.Rule
	s.db.Order("sort_order ASC, id ASC").Find(&rules)
	rulesByPolicy := map[uint][]matrixRule{}
	for _, r := range rules {
		rulesByPolicy[r.PolicyID] = append(rulesByPolicy[r.PolicyID], matrixRule{
			Type: r.Type, Value: r.Value, NoResolve: r.NoResolve, Disabled: r.Disabled,
		})
	}
	rows := make([]matrixPolicy, 0, len(policies))
	indexByPolicy := make(map[uint]int, len(policies))
	for i, p := range policies {
		indexByPolicy[p.ID] = i
		rows = append(rows, matrixPolicy{
			ID: p.ID, Name: p.Name, Icon: p.Icon, IsFinal: p.IsFinal,
			Enabled: p.Enabled, Remark: p.Remark, Rules: jsonList(rulesByPolicy[p.ID]),
		})
	}

	var egresses []model.Egress
	s.db.Find(&egresses)
	var members []model.EgressMember
	s.db.Order("sort_order ASC, id ASC").Find(&members)
	byEgress := map[uint][]matrixMember{}
	for _, m := range members {
		byEgress[m.EgressID] = append(byEgress[m.EgressID], matrixMember{Kind: m.Kind, Ref: m.Ref})
	}
	cells := make([]matrixEgress, 0, len(egresses))
	for _, e := range egresses {
		index, ok := indexByPolicy[e.PolicyID]
		if !ok {
			continue
		}
		cells = append(cells, matrixEgress{
			PolicyIndex: index, ProfileID: e.ProfileID, Type: e.Type,
			TestURL: e.TestURL, Interval: e.Interval, Tolerance: e.Tolerance,
			Hidden: e.Hidden, ClientHidden: e.ClientHidden, Members: jsonList(byEgress[e.ID]),
		})
	}

	var sets []model.RuleSet
	s.db.Order("id ASC").Find(&sets)
	ruleSets := make([]matrixRuleSet, 0, len(sets))
	for _, set := range sets {
		ruleSets = append(ruleSets, matrixRuleSet{ID: set.ID, Name: set.Name, Payload: set.Payload, Remark: set.Remark})
	}

	c.JSON(http.StatusOK, gin.H{
		"policies": rows,
		"egresses": cells,
		"ruleSets": ruleSets,
		// NeedsReview is set by the schema migration: the old flat rule list
		// was regrouped into policies and the order may not be what the
		// operator wants. Saving once clears it.
		"needsReview": s.db.GetSetting(store.SettingRoutingReview, "") != "",
		"options": gin.H{
			"ruleTypes":      routing.RuleTypes,
			"noResolveTypes": routing.NoResolveTypes,
			"groupTypes":     clash.GroupTypes,
			"builtins":       model.BuiltinPolicies,
			"memberKinds":    routing.MemberKinds,
			"presets":        routingPresets(),
		},
	})
}

type routingPreset struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func routingPresets() []routingPreset {
	out := make([]routingPreset, 0, len(routing.Presets))
	for _, p := range routing.Presets {
		out = append(out, routingPreset{Key: p.Key, Name: p.Name, Description: p.Description})
	}
	return out
}

// applyRoutingPreset swaps every policy for a built-in layout. Like a matrix
// save it validates inside the transaction, and like one it never touches a
// panel: profiles and their whitelists are kept as they are.
func (s *Server) applyRoutingPreset(c *gin.Context) {
	preset, ok := routing.PresetByKey(c.Param("key"))
	if !ok {
		failMsg(c, http.StatusNotFound, "没有这个分流预设")
		return
	}
	err := s.db.Transaction(func(handle *gorm.DB) error {
		if err := routing.ApplyPreset(tx(handle), preset); err != nil {
			return err
		}
		return routing.Validate(tx(handle))
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	_ = s.db.SetSetting(store.SettingRoutingReview, "")
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// setRouting replaces the whole matrix in one transaction. The editor always
// sends the complete table, and validating inside the transaction means a
// config mihomo would reject never reaches the database at all.
func (s *Server) setRouting(c *gin.Context) {
	var doc matrixDoc
	if err := c.ShouldBindJSON(&doc); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	err := s.db.Transaction(func(handle *gorm.DB) error {
		if err := writeMatrix(handle, &doc); err != nil {
			return err
		}
		return routing.Validate(tx(handle))
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	_ = s.db.SetSetting(store.SettingRoutingReview, "")
	// Nothing here changes which inbounds a user reaches, so the panels are
	// left alone; only the rendered subscription changes.
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func writeMatrix(handle *gorm.DB, doc *matrixDoc) error {
	keep := map[uint]bool{}
	for _, p := range doc.Policies {
		if p.ID != 0 {
			if keep[p.ID] {
				return fmt.Errorf("分流策略 ID %d 重复，请刷新页面后重试", p.ID)
			}
			keep[p.ID] = true
		}
	}
	// A cell copied from the default column may retain profileId 0. Reject
	// conflicting cells before replacing any rows, rather than leaking a SQL
	// unique-constraint error or silently choosing one of the two egresses.
	seenCells := make(map[struct {
		policyIndex int
		profileID   uint
	}]bool, len(doc.Egresses))
	for _, cell := range doc.Egresses {
		if cell.PolicyIndex < 0 || cell.PolicyIndex >= len(doc.Policies) {
			continue
		}
		key := struct {
			policyIndex int
			profileID   uint
		}{cell.PolicyIndex, cell.ProfileID}
		if seenCells[key] {
			column := "默认出口"
			if cell.ProfileID != model.DefaultProfileID {
				column = fmt.Sprintf("方案（ID %d）的出口", cell.ProfileID)
			}
			return fmt.Errorf("分流策略「%s」的%s重复，请刷新页面后重试", strings.TrimSpace(doc.Policies[cell.PolicyIndex].Name), column)
		}
		seenCells[key] = true
	}
	if doc.RuleSets != nil {
		renamedSets, err := writeRuleSets(handle, doc.RuleSets)
		if err != nil {
			return err
		}
		// RULE-SET rules name a set by its bare name, the same way members
		// name a policy.
		for i := range doc.Policies {
			for j := range doc.Policies[i].Rules {
				rule := &doc.Policies[i].Rules[j]
				if strings.TrimSpace(rule.Type) != routing.RuleSetType {
					continue
				}
				if next, ok := renamedSets[strings.TrimSpace(rule.Value)]; ok {
					rule.Value = next
				}
			}
		}
	}
	var existing []model.Policy
	if err := handle.Find(&existing).Error; err != nil {
		return err
	}
	// Members name a policy by its bare name, so a rename in this same save
	// has to travel to every member that pointed at the old name — whether
	// the editor rewrote them or not.
	renamed := map[string]string{}
	storedName := make(map[uint]string, len(existing))
	for _, p := range existing {
		storedName[p.ID] = p.Name
	}
	for _, p := range doc.Policies {
		if old, ok := storedName[p.ID]; ok && p.ID != 0 {
			if next := strings.TrimSpace(p.Name); next != old {
				renamed[old] = next
			}
		}
	}
	for i := range doc.Egresses {
		for j := range doc.Egresses[i].Members {
			m := &doc.Egresses[i].Members[j]
			if m.Kind != model.MemberPolicy {
				continue
			}
			if next, ok := renamed[strings.TrimSpace(m.Ref)]; ok {
				m.Ref = next
			}
		}
	}
	for _, p := range existing {
		if keep[p.ID] {
			continue
		}
		if err := deletePolicyRows(handle, p.ID); err != nil {
			return err
		}
	}

	idByIndex := make([]uint, len(doc.Policies))
	for i := range doc.Policies {
		row := &doc.Policies[i]
		policy := model.Policy{
			ID: row.ID, Name: strings.TrimSpace(row.Name), Icon: strings.TrimSpace(row.Icon),
			IsFinal: row.IsFinal, Enabled: row.Enabled, Remark: row.Remark,
			SortOrder: (i + 1) * 10,
		}
		if policy.ID == 0 {
			if err := createRow(handle, &policy); err != nil {
				return err
			}
		} else if err := handle.Model(&model.Policy{}).Where("id = ?", policy.ID).Updates(map[string]any{
			"name": policy.Name, "icon": policy.Icon, "is_final": policy.IsFinal,
			"enabled": policy.Enabled, "remark": policy.Remark, "sort_order": policy.SortOrder,
		}).Error; err != nil {
			return err
		}
		idByIndex[i] = policy.ID

		if err := handle.Where("policy_id = ?", policy.ID).Delete(&model.Rule{}).Error; err != nil {
			return err
		}
		for j, rule := range row.Rules {
			created := model.Rule{
				PolicyID: policy.ID, SortOrder: j, Type: strings.TrimSpace(rule.Type),
				Value: strings.TrimSpace(rule.Value), NoResolve: rule.NoResolve, Disabled: rule.Disabled,
			}
			if err := handle.Create(&created).Error; err != nil {
				return err
			}
		}
	}

	if err := clearEgresses(handle); err != nil {
		return err
	}
	for _, cell := range doc.Egresses {
		if cell.PolicyIndex < 0 || cell.PolicyIndex >= len(idByIndex) {
			continue
		}
		egress := model.Egress{
			PolicyID: idByIndex[cell.PolicyIndex], ProfileID: cell.ProfileID, Type: cell.Type,
			TestURL: strings.TrimSpace(cell.TestURL), Interval: cell.Interval,
			Tolerance: cell.Tolerance, Hidden: cell.Hidden, ClientHidden: cell.ClientHidden,
		}
		if egress.Type == "" {
			egress.Type = model.GroupTypeSelect
		}
		if err := handle.Create(&egress).Error; err != nil {
			return err
		}
		for j, member := range cell.Members {
			row := model.EgressMember{
				EgressID: egress.ID, SortOrder: j,
				Kind: member.Kind, Ref: strings.TrimSpace(member.Ref),
			}
			if err := handle.Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// writeRuleSets replaces the stored rule sets with the document's and returns
// the renames it made, old name to new.
func writeRuleSets(handle *gorm.DB, sets []matrixRuleSet) (map[string]string, error) {
	var existing []model.RuleSet
	if err := handle.Find(&existing).Error; err != nil {
		return nil, err
	}
	storedName := make(map[uint]string, len(existing))
	for _, set := range existing {
		storedName[set.ID] = set.Name
	}
	keep := map[uint]bool{}
	names := map[string]bool{}
	for _, set := range sets {
		name := strings.TrimSpace(set.Name)
		if name == "" {
			return nil, errBadRequest("有一个规则集没有名称")
		}
		if names[name] {
			return nil, fmt.Errorf("规则集名称「%s」重复", name)
		}
		names[name] = true
		if set.ID == 0 {
			continue
		}
		if _, ok := storedName[set.ID]; !ok || keep[set.ID] {
			return nil, fmt.Errorf("规则集「%s」已被修改或删除，请刷新页面后重试", name)
		}
		keep[set.ID] = true
	}
	for _, set := range existing {
		if !keep[set.ID] {
			if err := handle.Delete(&model.RuleSet{}, set.ID).Error; err != nil {
				return nil, err
			}
		}
	}

	// Names are unique, so a swap would collide midway; renamed rows step
	// aside first under a placeholder no valid name can equal, since valid
	// names have no commas.
	renamed := map[string]string{}
	for _, set := range sets {
		if set.ID == 0 {
			continue
		}
		if next := strings.TrimSpace(set.Name); next != storedName[set.ID] {
			renamed[storedName[set.ID]] = next
			if err := handle.Model(&model.RuleSet{}).Where("id = ?", set.ID).
				Update("name", fmt.Sprintf(",renaming,%d", set.ID)).Error; err != nil {
				return nil, err
			}
		}
	}
	for _, set := range sets {
		row := model.RuleSet{
			ID: set.ID, Name: strings.TrimSpace(set.Name),
			Payload: strings.ReplaceAll(set.Payload, "\r\n", "\n"), Remark: set.Remark,
		}
		if row.ID == 0 {
			if err := handle.Create(&row).Error; err != nil {
				return nil, err
			}
			continue
		}
		if err := handle.Model(&model.RuleSet{}).Where("id = ?", row.ID).Updates(map[string]any{
			"name": row.Name, "payload": row.Payload, "remark": row.Remark,
		}).Error; err != nil {
			return nil, err
		}
	}
	return renamed, nil
}

func deletePolicyRows(handle *gorm.DB, policyID uint) error {
	var egressIDs []uint
	handle.Model(&model.Egress{}).Where("policy_id = ?", policyID).Pluck("id", &egressIDs)
	if len(egressIDs) > 0 {
		if err := handle.Where("egress_id IN ?", egressIDs).Delete(&model.EgressMember{}).Error; err != nil {
			return err
		}
	}
	if err := handle.Where("policy_id = ?", policyID).Delete(&model.Egress{}).Error; err != nil {
		return err
	}
	if err := handle.Where("policy_id = ?", policyID).Delete(&model.Rule{}).Error; err != nil {
		return err
	}
	// Other policies may have listed this one as a member.
	var gone model.Policy
	if err := handle.First(&gone, policyID).Error; err == nil {
		handle.Where("kind = ? AND ref = ?", model.MemberPolicy, gone.Name).Delete(&model.EgressMember{})
	}
	return handle.Delete(&model.Policy{}, policyID).Error
}

func clearEgresses(handle *gorm.DB) error {
	if err := handle.Where("1 = 1").Delete(&model.EgressMember{}).Error; err != nil {
		return err
	}
	return handle.Where("1 = 1").Delete(&model.Egress{}).Error
}

// previewRouting renders what a client on this profile would receive. Proxy
// bodies are stand-ins — the real ones come from the panels at subscription
// time — but the groups and rules are exactly what would be served.
func (s *Server) previewRouting(c *gin.Context) {
	profileID := uint(0)
	if raw := c.Query("profileId"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			profileID = uint(parsed)
		}
	}
	// The default column has no whitelist of its own; previewing it over every
	// enabled group shows what a profile inherits before it narrows.
	var inbounds []model.Inbound
	var err error
	if profileID == model.DefaultProfileID {
		var groups []model.NodeGroup
		if groups, err = routing.AllGroups(s.db); err == nil {
			inbounds, err = routing.InboundsIn(s.db, groups)
		}
	} else {
		inbounds, err = routing.InboundsForProfile(s.db, profileID)
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	proxies := make([]routing.Proxy, 0, len(inbounds))
	used := map[string]bool{}
	for i := range inbounds {
		n := &inbounds[i]
		name := n.DisplayName()
		for j := 2; used[name]; j++ {
			name = n.DisplayName() + " #" + strconv.Itoa(j)
		}
		used[name] = true
		entry := clash.NewOrdered().Set("name", name).Set("type", strings.ToLower(n.Protocol))
		proxies = append(proxies, routing.Proxy{InboundID: n.ID, Name: name, Entry: entry})
	}

	// No user is behind a preview, so rule-set URLs carry a placeholder token.
	base := s.subBase(c)
	ruleSetURL := func(set model.RuleSet) string {
		return subscription.RuleSetURL(base, ":token", set.ID)
	}
	var in clash.Input
	if profileID == model.DefaultProfileID {
		in, err = routing.CompileDefault(s.db, proxies, ruleSetURL)
	} else {
		in, err = routing.Compile(s.db, profileID, proxies, ruleSetURL)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	template := s.db.GetSetting(subscription.SettingKeyClashTemplate, clash.DefaultTemplate)
	out, err := clash.Render(template, in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"yaml": out, "inbounds": len(inbounds)})
}
