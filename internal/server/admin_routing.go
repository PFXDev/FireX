package server

import (
	"net/http"
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

	out := make([]nodeGroupRow, 0, len(groups))
	for _, g := range groups {
		row := nodeGroupRow{
			NodeGroup:    g,
			Tags:         jsonList(tagsByGroup[g.ID]),
			InboundIDs:   jsonList(members[g.ID]),
			ProfileCount: profileCount[g.ID] + int(wildcards),
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
	if err := s.db.Create(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.setGroupTags(g.ID, req.Tags); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if err := s.setGroupMembers(g.ID, req.InboundIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// A brand-new group is reachable straight away by every profile that takes
	// all groups, so its inbounds have to be pushed.
	s.subs.InvalidateAll()
	s.reconcilePlans(routing.PlansUsingNodeGroup(s.db, g.ID))
	c.JSON(http.StatusOK, g)
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
	if err := req.apply(&g); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Save(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.setGroupTags(g.ID, req.Tags); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if err := s.setGroupMembers(g.ID, req.InboundIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
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
	s.reconcilePlans(routing.PlansUsingNodeGroup(s.db, g.ID))
	c.JSON(http.StatusOK, gin.H{"group": g, "rewrittenMembers": rewritten})
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

	s.db.Where("group_id = ?", g.ID).Delete(&model.NodeGroupInbound{})
	s.db.Where("group_id = ?", g.ID).Delete(&model.NodeGroupTag{})
	s.db.Where("group_id = ?", g.ID).Delete(&model.ProfileNodeGroup{})
	dropped := s.db.Where("kind = ? AND ref = ?", model.MemberNodeGroup, g.Name).
		Delete(&model.EgressMember{}).RowsAffected
	if err := s.db.Delete(&model.NodeGroup{}, g.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	s.reconcilePlans(plans)
	c.JSON(http.StatusOK, gin.H{"ok": true, "droppedMembers": dropped})
}

// setGroupMembers replaces a group's membership wholesale; the editor always
// sends the complete list.
func (s *Server) setGroupMembers(groupID uint, inboundIDs []uint) error {
	if err := s.db.Where("group_id = ?", groupID).Delete(&model.NodeGroupInbound{}).Error; err != nil {
		return err
	}
	if len(inboundIDs) == 0 {
		return nil
	}
	var existing []uint
	s.db.Model(&model.Inbound{}).Where("id IN ?", inboundIDs).Pluck("id", &existing)
	valid := map[uint]bool{}
	for _, id := range existing {
		valid[id] = true
	}
	links := make([]model.NodeGroupInbound, 0, len(inboundIDs))
	seen := map[uint]bool{}
	for _, id := range inboundIDs {
		if !valid[id] || seen[id] {
			continue
		}
		seen[id] = true
		links = append(links, model.NodeGroupInbound{GroupID: groupID, InboundID: id})
	}
	if len(links) == 0 {
		return nil
	}
	return s.db.Create(&links).Error
}

func (s *Server) setGroupTags(groupID uint, tags []nodeGroupTag) error {
	if err := s.db.Where("group_id = ?", groupID).Delete(&model.NodeGroupTag{}).Error; err != nil {
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
		if err := s.db.Create(&model.NodeGroupTag{GroupID: groupID, Key: key, Value: value}).Error; err != nil {
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
	if err := s.setProfileGroups(p.ID, req.GroupIDs); err != nil {
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
	if err := s.setProfileGroups(p.ID, req.GroupIDs); err != nil {
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
	s.db.Where("profile_id = ?", id).Delete(&model.ProfileNodeGroup{})
	// The profile's column of overrides goes with it; the default column stays.
	var egressIDs []uint
	s.db.Model(&model.Egress{}).Where("profile_id = ?", id).Pluck("id", &egressIDs)
	if len(egressIDs) > 0 {
		s.db.Where("egress_id IN ?", egressIDs).Delete(&model.EgressMember{})
		s.db.Where("id IN ?", egressIDs).Delete(&model.Egress{})
	}
	if err := s.db.Delete(&model.Profile{}, id).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) setProfileGroups(profileID uint, groupIDs []uint) error {
	if err := s.db.Where("profile_id = ?", profileID).Delete(&model.ProfileNodeGroup{}).Error; err != nil {
		return err
	}
	if len(groupIDs) == 0 {
		return nil
	}
	var existing []uint
	s.db.Model(&model.NodeGroup{}).Where("id IN ?", groupIDs).Pluck("id", &existing)
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
	return s.db.Create(&rows).Error
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
	ProfileID uint           `json:"profileId"`
	Type      string         `json:"type"`
	TestURL   string         `json:"testUrl"`
	Interval  int            `json:"interval"`
	Tolerance int            `json:"tolerance"`
	Hidden    bool           `json:"hidden"`
	Members   []matrixMember `json:"members"`
}

type matrixDoc struct {
	// Policies are in row order: it is both the rule precedence and the order
	// the groups appear in a client.
	Policies []matrixPolicy `json:"policies"`
	Egresses []matrixEgress `json:"egresses"`
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
			Hidden: e.Hidden, Members: jsonList(byEgress[e.ID]),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"policies": rows,
		"egresses": cells,
		"options": gin.H{
			"ruleTypes":      routing.RuleTypes,
			"noResolveTypes": routing.NoResolveTypes,
			"groupTypes":     clash.GroupTypes,
			"builtins":       model.BuiltinPolicies,
			"memberKinds":    routing.MemberKinds,
		},
	})
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
	// Nothing here changes which inbounds a user reaches, so the panels are
	// left alone; only the rendered subscription changes.
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func writeMatrix(handle *gorm.DB, doc *matrixDoc) error {
	keep := map[uint]bool{}
	for _, p := range doc.Policies {
		if p.ID != 0 {
			keep[p.ID] = true
		}
	}
	var existing []model.Policy
	if err := handle.Find(&existing).Error; err != nil {
		return err
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
			if err := handle.Create(&policy).Error; err != nil {
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
			Tolerance: cell.Tolerance, Hidden: cell.Hidden,
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
	inbounds, err := routing.InboundsForProfile(s.db, profileID)
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

	in, err := routing.Compile(s.db, profileID, proxies)
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
