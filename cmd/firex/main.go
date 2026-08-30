// Command firex runs the FireX control plane: it keeps a fleet of 3x-ui panels
// converged on one user model and serves each user's client subscription.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/server"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
	"github.com/PFXDev/FireX/internal/updater"
	"github.com/PFXDev/FireX/internal/version"
)

func main() {
	cfg := config.Load()
	log.Printf("firex: %s (commit=%s, built=%s)", version.Version, version.Commit, version.BuildTime)

	db, err := store.Open(cfg.DBPath, cfg.Debug)
	if err != nil {
		log.Fatalf("firex: %v", err)
	}
	defer db.Close()

	created, generated, err := server.EnsureAdmin(db, cfg.AdminUser, cfg.AdminPassword)
	if err != nil {
		log.Fatalf("firex: bootstrap admin: %v", err)
	}
	if created {
		if generated != "" {
			log.Printf("firex: created admin %q with generated password: %s", cfg.AdminUser, generated)
			log.Printf("firex: this password is shown once; change it after signing in")
		} else {
			log.Printf("firex: created admin %q from FIREX_ADMIN_PASSWORD", cfg.AdminUser)
		}
	}

	mgr := provision.NewManager(db)
	subs := subscription.NewService(db, mgr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Background work has its own cancel so an update can wind it down without
	// tripping the shutdown path below, which would race the restart.
	bgCtx, stopBackground := context.WithCancel(ctx)
	defer stopBackground()

	var srv *server.Server
	upd := updater.New(
		func() updater.Config { return cfg.Update },
		func() string { return cfg.DataDir },
		log.Default(),
		updater.RestartHooks{
			BeforeExec: func(tag string) error {
				// The successor process inherits this PID and needs the
				// listening socket and the SQLite WAL released first.
				stopBackground()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				if err := db.Close(); err != nil {
					return err
				}
				log.Printf("firex: prepared restart for %s", tag)
				return nil
			},
			OnExecFailure: func(err error) {
				// Everything is already torn down, so staying alive would only
				// look healthy to a supervisor. Exit and let it restart us.
				log.Printf("firex: restart failed after teardown: %v", err)
				stop()
			},
			IsBusy: mgr.IsBusy,
		},
	)
	srv = server.New(cfg, db, mgr, subs, upd)

	go runLoop(bgCtx, "discover", cfg.DiscoverInterval, 0, func(ctx context.Context) error {
		return mgr.DiscoverAll(ctx)
	})
	// Reconcile trails discovery so a freshly seen node can be provisioned in
	// the same cycle rather than the next one.
	go runLoop(bgCtx, "reconcile", cfg.SyncInterval, 15*time.Second, func(ctx context.Context) error {
		return mgr.ReconcileAll(ctx)
	})
	go runLoop(bgCtx, "traffic", cfg.TrafficInterval, 30*time.Second, func(ctx context.Context) error {
		return mgr.CollectTraffic(ctx)
	})
	upd.StartBackground(bgCtx)

	go func() {
		log.Printf("firex: listening on %s", cfg.Listen)
		if err := srv.Start(); err != nil {
			log.Fatalf("firex: server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("firex: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("firex: shutdown: %v", err)
	}
}

// runLoop runs fn every interval until ctx ends. Errors are logged rather than
// fatal: one unreachable panel must not take the control plane down.
func runLoop(ctx context.Context, name string, interval, delay time.Duration, fn func(context.Context) error) {
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		runCtx, cancel := context.WithTimeout(ctx, interval)
		if err := fn(runCtx); err != nil {
			log.Printf("firex: %s: %v", name, err)
		}
		cancel()
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
