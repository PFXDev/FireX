package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/subscription"
)

var errBadUsername = errors.New("username must be 2-40 characters of letters, digits, dash, underscore or dot")

// subBase is the public origin subscription URLs are built from. A configured
// value wins because FireX usually sits behind a reverse proxy that terminates
// TLS on a different host than the one it listens on.
func (s *Server) subBase(c *gin.Context) string {
	if base := s.cfg.Get().SubBaseURL; base != "" {
		return base
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
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
		"presets":   clash.TemplatePresets,
	})
}

// checkClashTemplate renders a template against a stand-in so a broken one is
// rejected when saved rather than when a client next fetches its subscription.
// Empty means the built-in default.
func checkClashTemplate(template string) error {
	if template == "" {
		return nil
	}
	probe := clash.Input{
		Proxies: []clash.Proxy{{Name: "probe", Entry: clash.NewOrdered().Set("name", "probe")}},
		Groups:  []clash.Group{{Name: "probe-group", Type: "select", Members: []string{"probe"}}},
		Rules:   []string{"MATCH,probe-group"},
	}
	_, err := clash.Render(template, probe)
	return err
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
	if err := checkClashTemplate(template); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	// A RULE-SET rule may name a provider only this template declares.
	err := s.db.Transaction(func(handle *gorm.DB) error {
		if err := tx(handle).SetSetting(subscription.SettingKeyClashTemplate, template); err != nil {
			return err
		}
		return routing.Validate(tx(handle))
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "isDefault": template == ""})
}
