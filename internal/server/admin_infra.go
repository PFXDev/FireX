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
	"github.com/PFXDev/FireX/internal/routing"
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
		InboundCount    int64 `json:"inboundCount"`
		EnabledInbounds int64 `json:"enabledInbounds"`
	}
	out := make([]row, 0, len(panels))
	for _, p := range panels {
		r := row{Panel: p}
		s.db.Model(&model.Inbound{}).Where("panel_id = ?", p.ID).Count(&r.InboundCount)
		s.db.Model(&model.Inbound{}).Where("panel_id = ? AND enabled = ? AND missing = ?", p.ID, true, false).Count(&r.EnabledInbounds)
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

	var inboundIDs []uint
	s.db.Model(&model.Inbound{}).Where("panel_id = ?", p.ID).Pluck("id", &inboundIDs)
	// Group membership has to go with the inbounds; a dangling row would keep
	// inflating every group's member count for good.
	if len(inboundIDs) > 0 {
		s.db.Where("inbound_id IN ?", inboundIDs).Delete(&model.NodeGroupInbound{})
	}
	s.db.Where("panel_id = ?", p.ID).Delete(&model.Inbound{})
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
	s.db.Model(&model.Inbound{}).Where("panel_id = ? AND missing = ?", p.ID, false).Count(&count)
	c.JSON(http.StatusOK, gin.H{"ok": true, "inbounds": count})
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

// ----------------------------------------------------------------- inbounds

type inboundRow struct {
	model.Inbound
	PanelName string `json:"panelName"`
	// GroupCount is how many node groups hold this inbound. Zero means it
	// reaches nobody, whatever its enabled flag says.
	GroupCount int64 `json:"groupCount"`
}

func (s *Server) listInbounds(c *gin.Context) {
	var inbounds []model.Inbound
	q := s.db.Order("sort_order ASC, id ASC")
	if panelID := c.Query("panelId"); panelID != "" {
		q = q.Where("panel_id = ?", panelID)
	}
	if err := q.Find(&inbounds).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	names := map[uint]string{}
	var panels []model.Panel
	s.db.Find(&panels)
	for _, p := range panels {
		names[p.ID] = p.Name
	}

	counts := map[uint]int64{}
	var tally []struct {
		InboundID uint
		N         int64
	}
	s.db.Model(&model.NodeGroupInbound{}).
		Select("inbound_id, COUNT(*) AS n").Group("inbound_id").Scan(&tally)
	for _, row := range tally {
		counts[row.InboundID] = row.N
	}

	out := make([]inboundRow, 0, len(inbounds))
	for _, n := range inbounds {
		out = append(out, inboundRow{Inbound: n, PanelName: names[n.PanelID], GroupCount: counts[n.ID]})
	}
	c.JSON(http.StatusOK, out)
}

type inboundRequest struct {
	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	SortOrder *int   `json:"sortOrder"`
	Enabled   *bool  `json:"enabled"`
	UDP       *bool  `json:"udp"`
}

func (s *Server) updateInbound(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req inboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var inbound model.Inbound
	if err := s.db.First(&inbound, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "inbound not found")
		return
	}
	wasEnabled := inbound.Enabled
	inbound.Name = strings.TrimSpace(req.Name)
	inbound.Emoji = strings.TrimSpace(req.Emoji)
	if req.SortOrder != nil {
		inbound.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		inbound.Enabled = *req.Enabled
	}
	if req.UDP != nil {
		inbound.UDP = *req.UDP
	}
	inbound.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&inbound).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	if wasEnabled != inbound.Enabled {
		s.reconcileInbound(inbound.ID)
	}
	c.JSON(http.StatusOK, inbound)
}

type inboundBulkRequest struct {
	IDs       []uint `json:"ids"`
	Enabled   *bool  `json:"enabled"`
	SortOrder *int   `json:"sortOrder"`
}

func (s *Server) bulkUpdateInbounds(c *gin.Context) {
	var req inboundBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.IDs) == 0 {
		failMsg(c, http.StatusBadRequest, "no inbounds selected")
		return
	}
	updates := map[string]any{"updated_at": provision.NowMs()}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if err := s.db.Model(&model.Inbound{}).Where("id IN ?", req.IDs).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	if req.Enabled != nil {
		// One pass for the whole selection: several inbounds usually reach the
		// same plans, and reconciling each separately would re-push every user
		// once per inbound.
		s.reconcileInbounds(req.IDs)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.IDs)})
}

// deleteInbound drops an inbound that no longer exists upstream. A live one must
// be removed on the panel first, or discovery would just recreate it.
func (s *Server) deleteInbound(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var inbound model.Inbound
	if err := s.db.First(&inbound, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "inbound not found")
		return
	}
	if !inbound.Missing {
		failMsg(c, http.StatusConflict, "inbound still exists on its panel; delete it there first")
		return
	}
	plans := routing.PlansUsingInbound(s.db, inbound.ID)
	s.db.Where("inbound_id = ?", inbound.ID).Delete(&model.NodeGroupInbound{})
	if err := s.db.Delete(&model.Inbound{}, inbound.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	s.reconcilePlans(plans)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// reconcileInbound re-pushes everyone whose profile reaches this inbound.
func (s *Server) reconcileInbound(inboundID uint) {
	s.reconcilePlans(routing.PlansUsingInbound(s.db, inboundID))
}

func (s *Server) reconcileInbounds(inboundIDs []uint) {
	var plans []uint
	for _, id := range inboundIDs {
		plans = append(plans, routing.PlansUsingInbound(s.db, id)...)
	}
	s.reconcilePlans(plans)
}

func (s *Server) reconcilePlans(planIDs []uint) {
	if len(planIDs) == 0 {
		return
	}
	ctx, cancel := opCtx()
	defer cancel()
	_ = s.mgr.ReconcileUsersOfPlans(ctx, planIDs)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
