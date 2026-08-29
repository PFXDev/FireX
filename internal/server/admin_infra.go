package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
)

// discoverTimeout bounds a single panel round-trip triggered from the UI.
const discoverTimeout = 30 * time.Second

// opTimeout bounds a whole admin operation that fans out to several panels.
const opTimeout = 5 * time.Minute

// opCtx detaches from the HTTP request: a browser that navigates away must not
// abort a reconcile halfway through, leaving panels disagreeing with FireX.
func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func paramID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		failMsg(c, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return uint(id), true
}

// ------------------------------------------------------------------- panels

type panelRequest struct {
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	APIToken      string `json:"apiToken"`
	SkipTLSVerify bool   `json:"skipTlsVerify"`
	Enabled       *bool  `json:"enabled"`
	Remark        string `json:"remark"`
}

func (s *Server) listPanels(c *gin.Context) {
	var panels []model.Panel
	if err := s.db.Order("id ASC").Find(&panels).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	type row struct {
		model.Panel
		NodeCount    int64 `json:"nodeCount"`
		EnabledNodes int64 `json:"enabledNodes"`
	}
	out := make([]row, 0, len(panels))
	for _, p := range panels {
		r := row{Panel: p}
		s.db.Model(&model.Node{}).Where("panel_id = ?", p.ID).Count(&r.NodeCount)
		s.db.Model(&model.Node{}).Where("panel_id = ? AND enabled = ? AND missing = ?", p.ID, true, false).Count(&r.EnabledNodes)
		// The token is a panel-wide admin credential; never echo it back.
		r.APIToken = ""
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) createPanel(c *gin.Context) {
	var req panelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.BaseURL) == "" || strings.TrimSpace(req.APIToken) == "" {
		failMsg(c, http.StatusBadRequest, "name, baseUrl and apiToken are required")
		return
	}
	now := provision.NowMs()
	p := model.Panel{
		Name:          strings.TrimSpace(req.Name),
		BaseURL:       strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		APIToken:      strings.TrimSpace(req.APIToken),
		SkipTLSVerify: req.SkipTLSVerify,
		Enabled:       req.Enabled == nil || *req.Enabled,
		Remark:        req.Remark,
		Status:        model.PanelStatusUnknown,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.db.Create(&p).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	discoverErr := s.mgr.DiscoverPanel(ctx, &p)
	p.APIToken = ""
	c.JSON(http.StatusOK, gin.H{"panel": p, "discoverError": errString(discoverErr)})
}

func (s *Server) updatePanel(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req panelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var p model.Panel
	if err := s.db.First(&p, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "panel not found")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		p.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.BaseURL) != "" {
		p.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	}
	// An empty token means "keep the existing one" — the UI never receives it
	// back, so it cannot resend it.
	if strings.TrimSpace(req.APIToken) != "" {
		p.APIToken = strings.TrimSpace(req.APIToken)
	}
	p.SkipTLSVerify = req.SkipTLSVerify
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	p.Remark = req.Remark
	p.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&p).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	s.mgr.Invalidate(p.ID)
	s.subs.InvalidateAll()
	p.APIToken = ""
	c.JSON(http.StatusOK, p)
}

func (s *Server) deletePanel(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var p model.Panel
	if err := s.db.First(&p, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "panel not found")
		return
	}

	// Best-effort cleanup of the clients FireX created there; a panel that is
	// already unreachable must not block removing it from FireX.
	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	var records []model.UserPanel
	s.db.Where("panel_id = ?", p.ID).Find(&records)
	client := s.mgr.ClientFor(&p)
	remoteErrors := 0
	for _, rec := range records {
		if err := client.DeleteClient(ctx, rec.Email); err != nil {
			remoteErrors++
		}
	}

	var nodeIDs []uint
	s.db.Model(&model.Node{}).Where("panel_id = ?", p.ID).Pluck("id", &nodeIDs)
	if len(nodeIDs) > 0 {
		s.db.Where("node_id IN ?", nodeIDs).Delete(&model.PlanNode{})
	}
	s.db.Where("panel_id = ?", p.ID).Delete(&model.Node{})
	s.db.Where("panel_id = ?", p.ID).Delete(&model.UserPanel{})
	if err := s.db.Delete(&model.Panel{}, p.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.mgr.Invalidate(p.ID)
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true, "remoteCleanupFailures": remoteErrors})
}

func (s *Server) discoverPanel(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var p model.Panel
	if err := s.db.First(&p, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "panel not found")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	if err := s.mgr.DiscoverPanel(ctx, &p); err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	var count int64
	s.db.Model(&model.Node{}).Where("panel_id = ? AND missing = ?", p.ID, false).Count(&count)
	c.JSON(http.StatusOK, gin.H{"ok": true, "nodes": count})
}

// testPanel checks credentials before the admin commits them.
func (s *Server) testPanel(c *gin.Context) {
	var req panelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	token := strings.TrimSpace(req.APIToken)
	if token == "" {
		// Re-testing an existing panel: the UI cannot resend a token it never got.
		var existing model.Panel
		if err := s.db.First(&existing, "base_url = ?", strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")).Error; err == nil {
			token = existing.APIToken
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	client := panel.New(strings.TrimSpace(req.BaseURL), token, req.SkipTLSVerify)
	status, err := client.Status(ctx)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	inbounds, err := client.Inbounds(ctx)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"xrayVersion":  status.Xray.Version,
		"panelVersion": status.PanelVersion,
		"inbounds":     len(inbounds),
	})
}

// -------------------------------------------------------------------- nodes

type nodeRow struct {
	model.Node
	PanelName string `json:"panelName"`
	PlanCount int64  `json:"planCount"`
}

func (s *Server) listNodes(c *gin.Context) {
	var nodes []model.Node
	q := s.db.Order("sort_order ASC, id ASC")
	if panelID := c.Query("panelId"); panelID != "" {
		q = q.Where("panel_id = ?", panelID)
	}
	if err := q.Find(&nodes).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	names := map[uint]string{}
	var panels []model.Panel
	s.db.Find(&panels)
	for _, p := range panels {
		names[p.ID] = p.Name
	}
	out := make([]nodeRow, 0, len(nodes))
	for _, n := range nodes {
		row := nodeRow{Node: n, PanelName: names[n.PanelID]}
		s.db.Model(&model.PlanNode{}).Where("node_id = ?", n.ID).Count(&row.PlanCount)
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

type nodeRequest struct {
	Name       string   `json:"name"`
	Region     string   `json:"region"`
	Emoji      string   `json:"emoji"`
	Tags       []string `json:"tags"`
	SortOrder  *int     `json:"sortOrder"`
	Enabled    *bool    `json:"enabled"`
	UDP        *bool    `json:"udp"`
	Multiplier *float64 `json:"multiplier"`
}

func (s *Server) updateNode(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req nodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var node model.Node
	if err := s.db.First(&node, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "node not found")
		return
	}
	wasEnabled := node.Enabled
	node.Name = req.Name
	node.Region = strings.TrimSpace(req.Region)
	node.Emoji = strings.TrimSpace(req.Emoji)
	node.Tags = strings.Join(req.Tags, ",")
	if req.SortOrder != nil {
		node.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		node.Enabled = *req.Enabled
	}
	if req.UDP != nil {
		node.UDP = *req.UDP
	}
	if req.Multiplier != nil {
		node.Multiplier = *req.Multiplier
	}
	node.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&node).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	if wasEnabled != node.Enabled {
		s.reconcileNodeUsers(node.ID)
	}
	c.JSON(http.StatusOK, node)
}

type nodeBulkRequest struct {
	IDs       []uint   `json:"ids"`
	Enabled   *bool    `json:"enabled"`
	Region    *string  `json:"region"`
	Tags      []string `json:"tags"`
	SortOrder *int     `json:"sortOrder"`
}

func (s *Server) bulkUpdateNodes(c *gin.Context) {
	var req nodeBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.IDs) == 0 {
		failMsg(c, http.StatusBadRequest, "no nodes selected")
		return
	}
	updates := map[string]any{"updated_at": provision.NowMs()}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Region != nil {
		updates["region"] = strings.TrimSpace(*req.Region)
	}
	if req.Tags != nil {
		updates["tags"] = strings.Join(req.Tags, ",")
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if err := s.db.Model(&model.Node{}).Where("id IN ?", req.IDs).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	if req.Enabled != nil {
		for _, id := range req.IDs {
			s.reconcileNodeUsers(id)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.IDs)})
}

// deleteNode drops a node whose inbound no longer exists upstream. A live node
// must be removed on the panel first, or discovery would just recreate it.
func (s *Server) deleteNode(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var node model.Node
	if err := s.db.First(&node, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "node not found")
		return
	}
	if !node.Missing {
		failMsg(c, http.StatusConflict, "node still exists on its panel; delete the inbound there first")
		return
	}
	s.db.Where("node_id = ?", node.ID).Delete(&model.PlanNode{})
	if err := s.db.Delete(&model.Node{}, node.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// reconcileNodeUsers re-pushes everyone whose plan includes this node.
func (s *Server) reconcileNodeUsers(nodeID uint) {
	ctx, cancel := opCtx()
	defer cancel()
	var planIDs []uint
	s.db.Model(&model.PlanNode{}).Where("node_id = ?", nodeID).Pluck("plan_id", &planIDs)
	for _, planID := range planIDs {
		_ = s.mgr.ReconcileUsersOfPlan(ctx, planID)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
