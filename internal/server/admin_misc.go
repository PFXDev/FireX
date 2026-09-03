package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/subscription"
)

var errBadUsername = errors.New("username must be 2-40 characters of letters, digits, dash, underscore or dot")

// subBase is the public origin subscription URLs are built from. A configured
// value wins because FireX usually sits behind a reverse proxy that terminates
// TLS on a different host than the one it listens on.
func (s *Server) subBase(c *gin.Context) string {
	if s.cfg.SubBaseURL != "" {
		return s.cfg.SubBaseURL
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func (s *Server) handleOverview(c *gin.Context) {
	type panelHealth struct {
		ID         uint   `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		LastError  string `json:"lastError"`
		LastSeenAt int64  `json:"lastSeenAt"`
	}
	var counts struct {
		Panels          int64 `json:"panels"`
		Inbounds        int64 `json:"inbounds"`
		EnabledInbounds int64 `json:"enabledInbounds"`
		MissingInbounds int64 `json:"missingInbounds"`
		NodeGroups      int64 `json:"nodeGroups"`
		Profiles        int64 `json:"profiles"`
		Plans           int64 `json:"plans"`
		Users           int64 `json:"users"`
		ActiveUsers     int64 `json:"activeUsers"`
	}
	s.db.Model(&model.Panel{}).Count(&counts.Panels)
	s.db.Model(&model.Inbound{}).Count(&counts.Inbounds)
	s.db.Model(&model.Inbound{}).Where("enabled = ? AND missing = ?", true, false).Count(&counts.EnabledInbounds)
	s.db.Model(&model.Inbound{}).Where("missing = ?", true).Count(&counts.MissingInbounds)
	s.db.Model(&model.NodeGroup{}).Count(&counts.NodeGroups)
	s.db.Model(&model.Profile{}).Count(&counts.Profiles)
	s.db.Model(&model.Plan{}).Count(&counts.Plans)
	s.db.Model(&model.User{}).Count(&counts.Users)

	var users []model.User
	s.db.Find(&users)
	now := nowMs()
	var totalUp, totalDown int64
	for _, u := range users {
		totalUp += u.Upload
		totalDown += u.Download
		if u.Active(now) {
			counts.ActiveUsers++
		}
	}

	var panels []model.Panel
	s.db.Order("id ASC").Find(&panels)
	health := make([]panelHealth, 0, len(panels))
	for _, p := range panels {
		health = append(health, panelHealth{p.ID, p.Name, p.Status, p.LastError, p.LastSeenAt})
	}

	var failed int64
	s.db.Model(&model.UserPanel{}).Where("state = ?", model.SyncStateFailed).Count(&failed)

	c.JSON(http.StatusOK, gin.H{
		"counts":      counts,
		"upload":      totalUp,
		"download":    totalDown,
		"failedSyncs": failed,
		"panels":      health,
	})
}

// handleSyncNow runs a full discover + reconcile pass on demand.
func (s *Server) handleSyncNow(c *gin.Context) {
	ctx, cancel := opCtx()
	defer cancel()
	discoverErr := s.mgr.DiscoverAll(ctx)
	reconcileErr := s.mgr.ReconcileAll(ctx)
	trafficErr := s.mgr.CollectTraffic(ctx)
	s.subs.InvalidateAll()
	c.JSON(http.StatusOK, gin.H{
		"discoverError":  errString(discoverErr),
		"reconcileError": errString(reconcileErr),
		"trafficError":   errString(trafficErr),
	})
}

func (s *Server) getClashTemplate(c *gin.Context) {
	template := s.db.GetSetting(subscription.SettingKeyClashTemplate, "")
	c.JSON(http.StatusOK, gin.H{
		"template":  template,
		"isDefault": template == "",
		"default":   clash.DefaultTemplate,
	})
}

type clashTemplateRequest struct {
	Template string `json:"template"`
}

func (s *Server) setClashTemplate(c *gin.Context) {
	var req clashTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	template := strings.TrimSpace(req.Template)
	if template != "" {
		// Render against a stand-in so a broken template is rejected here rather
		// than when a client next fetches its subscription.
		probe := clash.Input{
			Proxies: []clash.Proxy{{Name: "probe", Entry: clash.NewOrdered().Set("name", "probe")}},
			Groups:  []clash.Group{{Name: "probe-group", Type: "select", Members: []string{"probe"}}},
			Rules:   []string{"MATCH,probe-group"},
		}
		if _, err := clash.Render(template, probe); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
	}
	if err := s.db.SetSetting(subscription.SettingKeyClashTemplate, template); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "isDefault": template == ""})
}
