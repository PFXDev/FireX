package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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
	// Nil keeps the stored quota, so a client that predates them cannot
	// clear one by omission.
	TrafficLimit    *int64 `json:"trafficLimit"`
	TrafficResetDay *int   `json:"trafficResetDay"`
}

// applyPanelQuota copies the request's quota onto p, reporting whether it
// changed anything enforcement depends on.
func applyPanelQuota(c *gin.Context, p *model.Panel, req panelRequest, now int64) (changed, ok bool) {
	if req.TrafficLimit != nil {
		if *req.TrafficLimit < 0 {
			failMsg(c, http.StatusBadRequest, "trafficLimit must not be negative")
			return false, false
		}
		changed = *req.TrafficLimit != p.TrafficLimit
		p.TrafficLimit = *req.TrafficLimit
	}
	if req.TrafficResetDay != nil {
		day := *req.TrafficResetDay
		if day < 0 || day > 31 {
			failMsg(c, http.StatusBadRequest, "trafficResetDay must be between 0 and 31")
			return false, false
		}
		if day != p.TrafficResetDay {
			// Count the new schedule from now, as a plan does: measured from
			// the panel's creation, a passed boundary would wipe the period's
			// usage on the next poll.
			p.TrafficPeriodStart = now
		}
		p.TrafficResetDay = day
	}
	return changed, true
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
		// UserCount is how many FireX users hold a client here.
		UserCount int64 `json:"userCount"`
		// Used is Upload+Download; OverQuota means the panel's inbounds are
		// out of every subscription until NextTrafficReset (0 for never).
		Used             int64 `json:"used"`
		OverQuota        bool  `json:"overQuota"`
		NextTrafficReset int64 `json:"nextTrafficReset"`
	}
	out := make([]row, 0, len(panels))
	for _, p := range panels {
		r := row{
			Panel: p, Used: p.Upload + p.Download, OverQuota: p.OverQuota(),
			NextTrafficReset: s.mgr.NextPanelTrafficReset(&p),
		}
		s.db.Model(&model.Inbound{}).Where("panel_id = ?", p.ID).Count(&r.InboundCount)
		s.db.Model(&model.Inbound{}).Where("panel_id = ? AND enabled = ? AND missing = ?", p.ID, true, false).Count(&r.EnabledInbounds)
		s.db.Model(&model.UserPanel{}).Where("panel_id = ?", p.ID).Count(&r.UserCount)
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
	if _, ok := applyPanelQuota(c, &p, req, now); !ok {
		return
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
	quotaChanged, ok := applyPanelQuota(c, &p, req, p.UpdatedAt)
	if !ok {
		return
	}
	// The traffic collector owns the usage columns; an edit must not write
	// back the counters it read before a poll moved them.
	if err := s.db.Omit("upload", "download").Save(&p).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	s.mgr.Invalidate(p.ID)
	s.subs.InvalidateAll()

	// A raised or lifted quota has to bring the panel's users back now, and a
	// lowered one take them off, rather than on the next traffic poll.
	var syncErr error
	if quotaChanged {
		ctx, cancel := opCtx()
		defer cancel()
		syncErr = s.mgr.EnforceLimits(ctx)
	}
	p.APIToken = ""
	c.JSON(http.StatusOK, gin.H{"panel": p, "syncError": errString(syncErr)})
}

// resetPanelTraffic starts the panel's traffic period over now.
func (s *Server) resetPanelTraffic(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var p model.Panel
	if err := s.db.First(&p, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "panel not found")
		return
	}
	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ResetPanelTraffic(ctx, p.ID)
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true, "syncError": errString(syncErr)})
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

	var emails []string
	s.db.Model(&model.UserPanel{}).Where("panel_id = ?", p.ID).Pluck("email", &emails)
	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	remoteErrors := s.deleteRemoteClients(ctx, &p, emails)

	if err := deletePanelRows(s.db.DB, p.ID); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.mgr.Invalidate(p.ID)
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"ok": true, "remoteCleanupFailures": remoteErrors})
}

// deleteRemoteClients is the best-effort cleanup of the clients FireX created
// on a panel being removed; a panel that is already unreachable must not block
// removing it from FireX. It returns how many deletions failed.
func (s *Server) deleteRemoteClients(ctx context.Context, p *model.Panel, emails []string) int {
	client := s.mgr.ClientFor(p)
	failures := 0
	for _, email := range emails {
		if err := client.DeleteClient(ctx, email); err != nil {
			failures++
		}
	}
	return failures
}

// deletePanelRows removes a panel and everything FireX keeps about it.
func deletePanelRows(handle *gorm.DB, panelID uint) error {
	var inboundIDs []uint
	handle.Model(&model.Inbound{}).Where("panel_id = ?", panelID).Pluck("id", &inboundIDs)
	// Group membership has to go with the inbounds; a dangling row would keep
	// inflating every group's member count for good.
	if len(inboundIDs) > 0 {
		handle.Where("inbound_id IN ?", inboundIDs).Delete(&model.NodeGroupInbound{})
		handle.Where("inbound_id IN ?", inboundIDs).Delete(&model.InboundConfig{})
	}
	handle.Where("panel_id = ?", panelID).Delete(&model.Inbound{})
	handle.Where("panel_id = ?", panelID).Delete(&model.UserPanel{})
	handle.Where("panel_id = ?", panelID).Delete(&model.PlanPanelLimit{})
	return handle.Delete(&model.Panel{}, panelID).Error
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
	GroupCount      int64  `json:"groupCount"`
	VisionSupported bool   `json:"visionSupported"`
	VisionReason    string `json:"visionReason"`
	// ConfigState is empty for an inbound FireX does not manage.
	ConfigState string `json:"configState"`
	// ShareAddress is the host the panel advertises, used when PublicAddress
	// is empty. Empty when the panel derives it rather than storing one.
	ShareAddress string `json:"shareAddress"`
	// XHTTPMode is the server's XHTTP mode; empty is auto or not XHTTP.
	XHTTPMode string `json:"xhttpMode"`
}

func inboundVisionReason(n model.Inbound) string {
	if n.Missing {
		return "入站已失联，请先恢复并同步面板"
	}
	return panel.VisionReason(n.Protocol, n.Network, n.Security, n.DisableFlow)
}

func (s *Server) getInbound(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var inbound model.Inbound
	if err := s.db.First(&inbound, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "inbound not found")
		return
	}
	var config any
	if inbound.Config != "" {
		if err := json.Unmarshal([]byte(inbound.Config), &config); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"parameters": redactForViewer(c, config), "lastSeenAt": inbound.LastSeenAt})
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

	var configs []model.InboundConfig
	s.db.Find(&configs)
	managed := map[uint]*model.InboundConfig{}
	for i := range configs {
		managed[configs[i].InboundID] = &configs[i]
	}

	out := make([]inboundRow, 0, len(inbounds))
	for _, n := range inbounds {
		reason := inboundVisionReason(n)
		out = append(out, inboundRow{Inbound: n, PanelName: names[n.PanelID], GroupCount: counts[n.ID], VisionSupported: reason == "", VisionReason: reason, ConfigState: configState(n, managed[n.ID]), ShareAddress: panel.ShareAddress(n.Config), XHTTPMode: panel.XHTTPMode(n.Config)})
	}
	c.JSON(http.StatusOK, out)
}

type inboundRequest struct {
	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	SortOrder *int   `json:"sortOrder"`
	Enabled   *bool  `json:"enabled"`
	UDP       *bool  `json:"udp"`
	Vision    *bool  `json:"vision"`
	// XHTTPTuning only changes how auto-mode XHTTP inbounds render for mihomo.
	XHTTPTuning *bool `json:"xhttpTuning"`
	// PublicAddress and PublicPort override the endpoint the panel advertises.
	// Absent leaves the stored value; an empty string or zero clears it.
	PublicAddress *string `json:"publicAddress"`
	PublicPort    *int    `json:"publicPort"`
}

// validPublicAddress accepts a bare hostname or IP literal. Anything that
// looks like a URL or carries whitespace would end up verbatim in a share link
// and produce an entry every client rejects.
func validPublicAddress(addr string) bool {
	if addr == "" {
		return true
	}
	if strings.ContainsAny(addr, " \t\r\n/@:?#[]") {
		return net.ParseIP(addr) != nil
	}
	return true
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
	visionChanged := req.Vision != nil && (inbound.Vision == nil || *inbound.Vision != *req.Vision)
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
	if req.XHTTPTuning != nil {
		inbound.XHTTPTuning = *req.XHTTPTuning
	}
	if req.Vision != nil {
		if *req.Vision {
			if reason := inboundVisionReason(inbound); reason != "" {
				failMsg(c, http.StatusBadRequest, reason)
				return
			}
		}
		inbound.Vision = req.Vision
	}
	if req.PublicAddress != nil {
		addr := strings.TrimSpace(*req.PublicAddress)
		if !validPublicAddress(addr) {
			failMsg(c, http.StatusBadRequest, "publicAddress must be a hostname or IP address")
			return
		}
		// Repeating the panel's address would pin it here after the panel
		// moves; empty already follows the panel.
		if addr == panel.ShareAddress(inbound.Config) {
			addr = ""
		}
		inbound.PublicAddress = addr
	}
	if req.PublicPort != nil {
		if *req.PublicPort < 0 || *req.PublicPort > 65535 {
			failMsg(c, http.StatusBadRequest, "publicPort must be between 0 and 65535")
			return
		}
		inbound.PublicPort = *req.PublicPort
	}
	inbound.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&inbound).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var syncErr error
	if wasEnabled != inbound.Enabled || visionChanged {
		syncErr = s.reconcileInbound(inbound.ID)
	}
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{"inbound": inbound, "syncError": errString(syncErr)})
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
	var syncErr error
	if req.Enabled != nil {
		// One pass for the whole selection: several inbounds usually reach the
		// same plans, and reconciling each separately would re-push every user
		// once per inbound.
		syncErr = s.reconcileInbounds(req.IDs)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.IDs), "syncError": errString(syncErr)})
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
	if err := deleteInboundRows(s.db.DB, inbound.ID); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateAll()
	syncErr := s.reconcilePlans(plans)
	c.JSON(http.StatusOK, gin.H{"ok": true, "syncError": errString(syncErr)})
}

// deleteInboundRows removes an inbound with its group membership and owned
// configuration. Callers check first that it is gone upstream.
func deleteInboundRows(handle *gorm.DB, inboundID uint) error {
	handle.Where("inbound_id = ?", inboundID).Delete(&model.NodeGroupInbound{})
	handle.Where("inbound_id = ?", inboundID).Delete(&model.InboundConfig{})
	return handle.Delete(&model.Inbound{}, inboundID).Error
}

// reconcileInbound re-pushes everyone whose profile reaches this inbound.
func (s *Server) reconcileInbound(inboundID uint) error {
	return s.reconcilePlans(routing.PlansUsingInbound(s.db, inboundID))
}

func (s *Server) reconcileInbounds(inboundIDs []uint) error {
	var plans []uint
	for _, id := range inboundIDs {
		plans = append(plans, routing.PlansUsingInbound(s.db, id)...)
	}
	return s.reconcilePlans(plans)
}

// reconcilePlans pushes every user on these plans and returns the first panel
// failure. Callers put it in the response: the row is saved either way, but
// the operator has to know a panel disagrees with what they just saved.
func (s *Server) reconcilePlans(planIDs []uint) error {
	if len(planIDs) == 0 {
		return nil
	}
	ctx, cancel := opCtx()
	defer cancel()
	return s.mgr.ReconcileUsersOfPlans(ctx, planIDs)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
