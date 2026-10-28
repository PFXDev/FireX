// Package server exposes the FireX admin API, the client subscription
// endpoint, and the embedded management UI.
package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/backup"
	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
	"github.com/PFXDev/FireX/internal/updater"
	"github.com/PFXDev/FireX/internal/web"
)

type Server struct {
	cfg  *config.Live
	db   *store.DB
	mgr  *provision.Manager
	subs *subscription.Service
	upd  *updater.Updater
	bkup *backup.Service
	// restart re-executes FireX so a staged restore takes over; tests swap it
	// out rather than exec the test binary.
	restart    func(reason string) error
	configPath string
	configMu   sync.Mutex
	// agentMu orders submissions against merges, so a merge never applies a
	// submission the reviewer has not seen.
	agentMu sync.Mutex

	engine *gin.Engine
	http   *http.Server
}

func New(cfg *config.Live, configPath string, db *store.DB, mgr *provision.Manager, subs *subscription.Service, upd *updater.Updater, bkup *backup.Service) *Server {
	if !cfg.Get().Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	s := &Server{cfg: cfg, configPath: configPath, db: db, mgr: mgr, subs: subs, upd: upd, bkup: bkup, restart: upd.Restart}
	s.engine = gin.New()
	s.engine.Use(gin.Recovery())
	// The request log is always mounted and consults debug per request, so
	// switching debug in the config takes effect without a restart.
	requestLog := gin.Logger()
	s.engine.Use(func(c *gin.Context) {
		if s.cfg.Get().Debug {
			requestLog(c)
			return
		}
		c.Next()
	})
	s.routes()
	return s
}

func (s *Server) routes() {
	// Subscriptions are unauthenticated by design: the token in the path is the
	// credential, so it must never be logged alongside a session cookie.
	s.engine.GET("/sub/:token", s.handleSubscription)
	s.engine.HEAD("/sub/:token", s.handleSubscription)
	s.engine.GET("/sub/:token/rules/:id", s.handleRuleSet)
	s.engine.HEAD("/sub/:token/rules/:id", s.handleRuleSet)

	api := s.engine.Group("/api")
	api.POST("/auth/login", s.handleLogin)
	api.POST("/auth/logout", s.handleLogout)

	// Every successful write through the admin API lands in the audit log.
	authed := api.Group("", s.requireAdmin, s.audit)
	authed.GET("/auth/me", s.handleMe)
	authed.POST("/auth/password", s.handleChangePassword)

	// Admins manage other admins, so the group is closed to operators.
	admins := authed.Group("/admins", s.requireSuperAdmin)
	admins.GET("", s.listAdmins)
	admins.POST("", s.createAdmin)
	admins.PUT("/:id", s.updateAdmin)
	admins.DELETE("/:id", s.deleteAdmin)
	admins.POST("/:id/resetPassword", s.resetAdminPasswordHandler)

	// Overview and the plan list are the operator's window into the install;
	// everything that changes infrastructure, routing or accounts is closed to
	// them. Users are the one thing both roles manage.
	authed.GET("/overview", s.handleOverview)
	authed.POST("/sync", s.superAdminOnly, s.handleSyncNow)

	// An agent session hands the configuration to an outside agent: it edits
	// a snapshot with a single-use token, and an admin reviews the diff before
	// anything takes effect.
	authed.GET("/agent-sessions", s.superAdminOnly, s.listAgentSessions)
	authed.POST("/agent-sessions", s.superAdminOnly, s.createAgentSession)
	authed.GET("/agent-sessions/current", s.superAdminOnly, s.currentAgentSession)
	authed.GET("/agent-sessions/:id", s.superAdminOnly, s.getAgentSession)
	authed.POST("/agent-sessions/:id/merge", s.superAdminOnly, s.mergeAgentSession)
	authed.POST("/agent-sessions/:id/return", s.superAdminOnly, s.returnAgentSession)
	authed.POST("/agent-sessions/:id/cancel", s.superAdminOnly, s.cancelAgentSession)

	// The agent's own surface. Its token works nowhere else and only while
	// the session is live.
	agent := api.Group("/agent", s.requireAgent)
	agent.GET("", s.agentSession)
	agent.GET("/guide", s.agentGuide)
	agent.GET("/snapshot", s.agentSnapshot)
	agent.POST("/preview", s.agentPreview)
	agent.PUT("/submission", s.agentSubmit)

	// Configuration is written by super admins only. Reads also open to an
	// operator whose "view configuration" switch is on, with inbound secrets
	// masked; see redactForViewer.
	authed.GET("/panels", s.configViewer, s.listPanels)
	authed.POST("/panels", s.superAdminOnly, s.createPanel)
	authed.PUT("/panels/:id", s.superAdminOnly, s.updatePanel)
	authed.DELETE("/panels/:id", s.superAdminOnly, s.deletePanel)
	authed.POST("/panels/:id/discover", s.superAdminOnly, s.discoverPanel)
	authed.POST("/panels/:id/resetTraffic", s.superAdminOnly, s.resetPanelTraffic)
	authed.POST("/panels/test", s.superAdminOnly, s.testPanel)

	authed.GET("/inbounds", s.configViewer, s.listInbounds)
	authed.GET("/inbounds/:id", s.configViewer, s.getInbound)
	authed.PUT("/inbounds/:id", s.superAdminOnly, s.updateInbound)
	authed.POST("/inbounds/bulk", s.superAdminOnly, s.bulkUpdateInbounds)
	authed.DELETE("/inbounds/:id", s.superAdminOnly, s.deleteInbound)
	authed.GET("/inbounds/:id/config", s.configViewer, s.getInboundConfig)
	authed.POST("/inbounds/:id/config", s.superAdminOnly, s.adoptInboundConfig)
	authed.PUT("/inbounds/:id/config", s.superAdminOnly, s.updateInboundConfig)
	authed.DELETE("/inbounds/:id/config", s.superAdminOnly, s.releaseInboundConfig)
	authed.POST("/inbounds/:id/config/preview", s.superAdminOnly, s.previewInboundConfig)
	authed.POST("/inbounds/:id/config/deploy", s.superAdminOnly, s.deployInboundConfig)
	authed.GET("/inbound-templates", s.configViewer, s.listInboundTemplates)
	authed.POST("/inbound-templates", s.superAdminOnly, s.createInboundTemplate)
	authed.GET("/inbound-templates/:id", s.configViewer, s.getInboundTemplate)
	authed.PUT("/inbound-templates/:id", s.superAdminOnly, s.updateInboundTemplate)
	authed.DELETE("/inbound-templates/:id", s.superAdminOnly, s.deleteInboundTemplate)
	authed.POST("/inbound-templates/:id/instantiate", s.superAdminOnly, s.instantiateInboundTemplate)

	authed.GET("/node-groups", s.configViewer, s.listNodeGroups)
	authed.POST("/node-groups", s.superAdminOnly, s.createNodeGroup)
	authed.PUT("/node-groups/:id", s.superAdminOnly, s.updateNodeGroup)
	authed.DELETE("/node-groups/:id", s.superAdminOnly, s.deleteNodeGroup)

	// Operators read profiles to render the plan list, but never reshape them.
	authed.GET("/profiles", s.listProfiles)
	authed.POST("/profiles", s.superAdminOnly, s.createProfile)
	authed.PUT("/profiles/:id", s.superAdminOnly, s.updateProfile)
	authed.DELETE("/profiles/:id", s.superAdminOnly, s.deleteProfile)

	// The matrix is read and written whole: partial saves would let a rule
	// point at a policy the same edit removed.
	authed.GET("/routing", s.configViewer, s.getRouting)
	authed.PUT("/routing", s.superAdminOnly, s.setRouting)
	authed.GET("/routing/preview", s.configViewer, s.previewRouting)
	authed.POST("/routing/presets/:key", s.superAdminOnly, s.applyRoutingPreset)

	authed.GET("/plans", s.listPlans)
	authed.POST("/plans", s.superAdminOnly, s.createPlan)
	authed.PUT("/plans/:id", s.superAdminOnly, s.updatePlan)
	authed.DELETE("/plans/:id", s.superAdminOnly, s.deletePlan)

	authed.GET("/users", s.listUsers)
	authed.POST("/users", s.createUser)
	authed.PUT("/users/:id", s.updateUser)
	authed.DELETE("/users/:id", s.deleteUser)
	authed.POST("/users/:id/resync", s.resyncUser)
	authed.POST("/users/:id/resetTraffic", s.resetUserTraffic)
	authed.GET("/users/:id/subscription", s.previewSubscription)

	authed.GET("/settings/clashTemplate", s.configViewer, s.getClashTemplate)
	authed.PUT("/settings/clashTemplate", s.superAdminOnly, s.setClashTemplate)
	authed.GET("/settings/server", s.superAdminOnly, s.getServerConfig)
	authed.PUT("/settings/server", s.superAdminOnly, s.setServerConfig)

	// Self-update sits behind the admin session like everything else. The
	// session lives in the database, so it survives the restart an update
	// causes and the UI keeps polling straight through.
	authed.GET("/audit-logs", s.superAdminOnly, s.listAuditLogs)

	authed.GET("/backups", s.superAdminOnly, s.listBackups)
	authed.POST("/backups", s.superAdminOnly, s.createBackup)
	authed.POST("/backups/upload", s.superAdminOnly, s.uploadBackup)
	authed.GET("/backups/:name/download", s.superAdminOnly, s.downloadBackup)
	authed.DELETE("/backups/:name", s.superAdminOnly, s.deleteBackup)
	authed.POST("/backups/:name/restore", s.superAdminOnly, s.restoreBackup)

	authed.GET("/version", s.superAdminOnly, s.handleVersion)
	authed.GET("/update/status", s.superAdminOnly, s.handleUpdateStatus)
	authed.POST("/update/check", s.superAdminOnly, s.handleUpdateCheck)
	authed.POST("/update/apply", s.superAdminOnly, s.handleUpdateApply)
	authed.POST("/update/dismiss", s.superAdminOnly, s.handleUpdateDismiss)

	s.mountUI()
}

// mountUI serves the embedded SPA, falling back to index.html so client-side
// routes survive a page reload.
func (s *Server) mountUI() {
	dist, err := web.Dist()
	if err != nil {
		s.engine.NoRoute(func(c *gin.Context) {
			c.String(http.StatusNotFound, "FireX UI is not built into this binary; run `make ui` and rebuild.")
		})
		return
	}
	fileServer := http.FileServer(http.FS(dist))
	s.engine.NoRoute(func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				c.Request.URL.Path = "/"
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

func (s *Server) Start() error {
	s.http = &http.Server{
		Addr:              s.cfg.Get().Listen,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func fail(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

func failMsg(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// errBadRequest carries a message written for the admin's screen rather than a
// log line, so handlers can hand it straight to fail().
func errBadRequest(msg string) error { return errors.New(msg) }

func nowMs() int64 { return provision.NowMs() }
