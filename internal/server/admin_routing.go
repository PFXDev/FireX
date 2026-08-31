package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/subscription"
)

var (
	errBadGroupName   = errors.New("group name is required")
	errGroupNameComma = errors.New("group name and emoji must not contain a comma")
	errBadGroupType   = errors.New("group type must be one of select, url-test, fallback, load-balance")
)

// -------------------------------------------------------------- node groups

type nodeGroupRow struct {
	model.NodeGroup
	NodeIDs []uint `json:"nodeIds"`
	// EnabledNodes counts the members a subscription would actually render, so
	// a group that looks populated but ships nothing is visible in the list.
	EnabledNodes int `json:"enabledNodes"`
}

func (s *Server) listNodeGroups(c *gin.Context) {
	var groups []model.NodeGroup
	if err := s.db.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var links []model.NodeGroupNode
	s.db.Find(&links)
	members := map[uint][]uint{}
	for _, link := range links {
		members[link.GroupID] = append(members[link.GroupID], link.NodeID)
	}

	var nodes []model.Node
	s.db.Find(&nodes)
	renderable := map[uint]bool{}
	for _, n := range nodes {
		renderable[n.ID] = n.Enabled && !n.Missing
	}

	out := make([]nodeGroupRow, 0, len(groups))
	for _, g := range groups {
		row := nodeGroupRow{NodeGroup: g, NodeIDs: members[g.ID]}
		if row.NodeIDs == nil {
			row.NodeIDs = []uint{}
		}
		for _, id := range row.NodeIDs {
			if renderable[id] {
				row.EnabledNodes++
			}
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

type nodeGroupRequest struct {
	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	Region    string `json:"region"`
	Line      string `json:"line"`
	Type      string `json:"type"`
	TestURL   string `json:"testUrl"`
	Interval  *int   `json:"interval"`
	Tolerance *int   `json:"tolerance"`
	SortOrder *int   `json:"sortOrder"`
	Enabled   *bool  `json:"enabled"`
	Remark    string `json:"remark"`
	NodeIDs   []uint `json:"nodeIds"`
}

// apply copies the request onto a group, rejecting the shapes that would
// produce a config mihomo cannot load.
func (req *nodeGroupRequest) apply(g *model.NodeGroup) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errBadGroupName
	}
	// Rules are comma-separated lines; a comma anywhere in a rendered group
	// name splits them into nonsense.
	if strings.Contains(name, ",") || strings.Contains(req.Emoji, ",") {
		return errGroupNameComma
	}
	groupType := strings.TrimSpace(req.Type)
	if groupType == "" {
		groupType = model.GroupTypeURLTest
	}
	if !clashHasType(groupType) {
		return errBadGroupType
	}
	g.Name = name
	g.Emoji = strings.TrimSpace(req.Emoji)
	g.Region = strings.TrimSpace(req.Region)
	g.Line = strings.TrimSpace(req.Line)
	g.Type = groupType
	g.TestURL = strings.TrimSpace(req.TestURL)
	g.Remark = req.Remark
	if req.Interval != nil {
		g.Interval = *req.Interval
	}
	if req.Tolerance != nil {
		g.Tolerance = *req.Tolerance
	}
	if req.SortOrder != nil {
		g.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		g.Enabled = *req.Enabled
	}
	return nil
}

func clashHasType(t string) bool {
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
	now := provision.NowMs()
	g := model.NodeGroup{
		Type: model.GroupTypeURLTest, Interval: 300, Tolerance: 50,
		SortOrder: 100, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := req.apply(&g); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.db.Create(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.setGroupMembers(g.ID, req.NodeIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
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
	g.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&g).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.setGroupMembers(g.ID, req.NodeIDs); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// The routing model references groups by name, so a rename has to travel
	// with it or every rule pointing here would dangle.
	rewritten := 0
	if previousName != g.Name {
		_, rewritten = s.rewriteRoutingRefs(previousName, g.Name)
	}
	c.JSON(http.StatusOK, gin.H{"group": g, "rewrittenRules": rewritten})
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
	s.db.Where("group_id = ?", g.ID).Delete(&model.NodeGroupNode{})
	if err := s.db.Delete(&model.NodeGroup{}, g.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	droppedMembers, droppedRules := s.rewriteRoutingRefs(g.Name, "")
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"droppedMembers": droppedMembers,
		"droppedRules":   droppedRules,
	})
}

// setGroupMembers replaces a group's membership wholesale; the editor always
// sends the complete list.
func (s *Server) setGroupMembers(groupID uint, nodeIDs []uint) error {
	if err := s.db.Where("group_id = ?", groupID).Delete(&model.NodeGroupNode{}).Error; err != nil {
		return err
	}
	if len(nodeIDs) == 0 {
		return nil
	}
	var existing []uint
	s.db.Model(&model.Node{}).Where("id IN ?", nodeIDs).Pluck("id", &existing)
	valid := map[uint]bool{}
	for _, id := range existing {
		valid[id] = true
	}
	links := make([]model.NodeGroupNode, 0, len(nodeIDs))
	seen := map[uint]bool{}
	for _, id := range nodeIDs {
		if !valid[id] || seen[id] {
			continue
		}
		seen[id] = true
		links = append(links, model.NodeGroupNode{GroupID: groupID, NodeID: id})
	}
	if len(links) == 0 {
		return nil
	}
	return s.db.Create(&links).Error
}

// rewriteRoutingRefs keeps the stored routing in step with a group rename or
// deletion. A stored model is only rewritten if one was ever saved: the
// built-in default references no node groups, so it has nothing to fix.
func (s *Server) rewriteRoutingRefs(old, replacement string) (members, rules int) {
	routing, stored := subscription.Routing(s.db)
	if !stored {
		return 0, 0
	}
	members, rules = routing.RenameNodeGroup(old, replacement)
	if members == 0 && rules == 0 {
		return 0, 0
	}
	encoded, err := json.Marshal(routing)
	if err != nil {
		return 0, 0
	}
	if err := s.db.SetSetting(subscription.SettingKeyRouting, string(encoded)); err != nil {
		return 0, 0
	}
	return members, rules
}

// ------------------------------------------------------------------ routing

func (s *Server) getRouting(c *gin.Context) {
	routing, stored := subscription.Routing(s.db)
	c.JSON(http.StatusOK, gin.H{
		"mode":      subscription.Mode(s.db),
		"routing":   routing,
		"isDefault": !stored,
		"default":   clash.DefaultRouting(),
		"options": gin.H{
			"groupTypes": clash.GroupTypes,
			"ruleTypes":  clash.RuleTypes,
			"builtins":   clash.BuiltinPolicies,
		},
	})
}

type routingRequest struct {
	Mode    string         `json:"mode"`
	Routing *clash.Routing `json:"routing"`
}

func (s *Server) setRouting(c *gin.Context) {
	var req routingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	mode := subscription.ModeVisual
	if req.Mode == subscription.ModeYAML {
		mode = subscription.ModeYAML
	}
	if req.Routing != nil {
		if err := req.Routing.Validate(s.nodeGroupNames()); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		// Render once against the real base template: a routing model that
		// validates can still meet a template that does not parse.
		if _, err := s.renderRoutingPreview(req.Routing); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		encoded, err := json.Marshal(req.Routing)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		if err := s.db.SetSetting(subscription.SettingKeyRouting, string(encoded)); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.db.SetSetting(subscription.SettingKeyMode, mode); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "mode": mode})
}

// resetRouting drops the stored model so the built-in default applies again.
func (s *Server) resetRouting(c *gin.Context) {
	if err := s.db.SetSetting(subscription.SettingKeyRouting, ""); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// previewRouting renders the routing an admin is editing against the real node
// groups, so they can read the config a client would receive before saving it.
func (s *Server) previewRouting(c *gin.Context) {
	var req routingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	routing := req.Routing
	if routing == nil {
		routing, _ = subscription.Routing(s.db)
	}
	if err := routing.Validate(s.nodeGroupNames()); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	out, err := s.renderRoutingPreview(routing)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"yaml": out})
}

// renderRoutingPreview renders every enabled node as if one user held them
// all. Proxy bodies are stand-ins — they come from the panels at subscription
// time — but the groups and rules are exactly what a client would get.
func (s *Server) renderRoutingPreview(routing *clash.Routing) (string, error) {
	var nodes []model.Node
	s.db.Where("enabled = ? AND missing = ?", true, false).Order("sort_order ASC, id ASC").Find(&nodes)

	entries := make([]subscription.Entry, 0, len(nodes))
	rendered := make([]clash.Node, 0, len(nodes))
	used := map[string]bool{}
	for _, n := range nodes {
		name := n.DisplayName()
		for i := 2; used[name]; i++ {
			name = n.DisplayName() + " #" + strconv.Itoa(i)
		}
		used[name] = true
		entry := clash.NewOrdered().Set("name", name).Set("type", strings.ToLower(n.Protocol))
		entries = append(entries, subscription.Entry{Node: n, Name: name, Clash: entry})
		rendered = append(rendered, clash.Node{Name: name, Region: n.Region, Entry: entry})
	}

	template := s.db.GetSetting(subscription.SettingKeyClashTemplate, clash.DefaultTemplate)
	return clash.Render(template, clash.Input{
		Nodes:   rendered,
		Groups:  subscription.GroupsFor(s.db, entries),
		Routing: routing,
	})
}

// generateNodeGroups materialises the bootstrap region split as real rows, so
// an admin can start from the automatic grouping and then edit it. Regions
// that already have a group of the same name are left alone.
func (s *Server) generateNodeGroups(c *gin.Context) {
	var nodes []model.Node
	s.db.Where("enabled = ? AND missing = ?", true, false).Order("sort_order ASC, id ASC").Find(&nodes)
	regions, members := subscription.RegionSplit(nodes)

	var existing []string
	s.db.Model(&model.NodeGroup{}).Pluck("name", &existing)
	taken := map[string]bool{}
	for _, name := range existing {
		taken[name] = true
	}

	now := provision.NowMs()
	created := 0
	for i, region := range regions {
		if taken[region] || strings.Contains(region, ",") {
			continue
		}
		g := model.NodeGroup{
			Name: region, Region: region,
			Type: model.GroupTypeURLTest, Interval: 300, Tolerance: 50,
			SortOrder: 100 + i, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.db.Create(&g).Error; err != nil {
			continue
		}
		if err := s.setGroupMembers(g.ID, members[region]); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		created++
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "created": created, "regions": len(regions)})
}

func (s *Server) nodeGroupNames() []string {
	var names []string
	s.db.Model(&model.NodeGroup{}).Order("sort_order ASC, id ASC").Pluck("name", &names)
	return names
}
