// Package server exposes the FireX admin API, the client subscription
// endpoint, and the embedded management UI.
package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
	"github.com/PFXDev/FireX/web"
)

type Server struct {
	cfg  *config.Config
	db   *store.DB
	mgr  *provision.Manager
	subs *subscription.Service

	engine *gin.Engine
	http   *http.Server
}

func New(cfg *config.Config, db *store.DB, mgr *provision.Manager, subs *subscription.Service) *Server {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	s := &Server{cfg: cfg, db: db, mgr: mgr, subs: subs}
	s.engine = gin.New()
	s.engine.Use(gin.Recovery())
	if cfg.Debug {
		s.engine.Use(gin.Logger())
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Subscriptions are unauthenticated by design: the token in the path is the
	// credential, so it must never be logged alongside a session cookie.
	s.engine.GET("/sub/:token", s.handleSubscription)
	s.engine.HEAD("/sub/:token", s.handleSubscription)

	api := s.engine.Group("/api")
	api.POST("/auth/login", s.handleLogin)
	api.POST("/auth/logout", s.handleLogout)

	authed := api.Group("", s.requireAdmin)
	authed.GET("/auth/me", s.handleMe)
	authed.POST("/auth/password", s.handleChangePassword)

	authed.GET("/overview", s.handleOverview)
	authed.POST("/sync", s.handleSyncNow)

	authed.GET("/panels", s.listPanels)
	authed.POST("/panels", s.createPanel)
	authed.PUT("/panels/:id", s.updatePanel)
	authed.DELETE("/panels/:id", s.deletePanel)
	authed.POST("/panels/:id/discover", s.discoverPanel)
	authed.POST("/panels/test", s.testPanel)

	authed.GET("/nodes", s.listNodes)
	authed.PUT("/nodes/:id", s.updateNode)
	authed.POST("/nodes/bulk", s.bulkUpdateNodes)
	authed.DELETE("/nodes/:id", s.deleteNode)

	authed.GET("/plans", s.listPlans)
	authed.POST("/plans", s.createPlan)
	authed.PUT("/plans/:id", s.updatePlan)
	authed.DELETE("/plans/:id", s.deletePlan)

	authed.GET("/users", s.listUsers)
	authed.POST("/users", s.createUser)
	authed.PUT("/users/:id", s.updateUser)
	authed.DELETE("/users/:id", s.deleteUser)
	authed.POST("/users/:id/resync", s.resyncUser)
	authed.POST("/users/:id/resetTraffic", s.resetUserTraffic)
	authed.GET("/users/:id/subscription", s.previewSubscription)

	authed.GET("/settings/clashTemplate", s.getClashTemplate)
	authed.PUT("/settings/clashTemplate", s.setClashTemplate)

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
		Addr:              s.cfg.Listen,
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

func nowMs() int64 { return provision.NowMs() }
