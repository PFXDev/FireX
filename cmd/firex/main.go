// Command firex runs the FireX control plane: it keeps a fleet of 3x-ui panels
// converged on one user model and serves each user's client subscription.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PFXDev/FireX/internal/backup"
	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/server"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
	"github.com/PFXDev/FireX/internal/updater"
	"github.com/PFXDev/FireX/internal/version"
)

func main() {
	// The path is a flag rather than a setting because it is the one thing the
	// config file cannot tell us. An update re-executes with this same argv, so
	// a non-default location survives the restart.
	configPath := flag.String("config", config.DefaultPath, "path to the JSON config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("firex: %v", err)
	}
	log.Printf("firex: %s (commit=%s, built=%s)", version.Version, version.Commit, version.BuildTime)

	db, err := store.Open(cfg.DBPath, cfg.Debug)
	if err != nil {
		log.Fatalf("firex: %v", err)
	}
	defer db.Close()

	// A database with no policies has no way to route anything, so a fresh
	// install gets the stock matrix. Deleting a policy later does not bring it
	// back on the next restart.
	if err := routing.Seed(db); err != nil {
		log.Fatalf("firex: seed routing: %v", err)
	}

	// The config's adminPassword is a one-shot override — creating the admin
	// on first start, resetting it after — and is blanked from the file once
	// applied, so this is also the way back in from a lost password.
	if err := server.BootstrapAdmin(db, cfg, *configPath); err != nil {
		log.Fatalf("firex: bootstrap admin: %v", err)
	}

	// Settings other than the listener and storage paths can change while the
	// process runs, so everything past startup reads them through live.
	live := config.NewLive(cfg)
	mgr := provision.NewManager(db)
	subs := subscription.NewService(db, mgr)
	// The data directory and database path are bound at startup, so the
	// backup directory is too; only the schedule follows the live config.
	backups := backup.New(db, cfg.DBPath, cfg.DataDir, func() config.Backup { return live.Get().Backup }, log.Default())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Background work has its own cancel so an update can wind it down without
	// tripping the shutdown path below, which would race the restart.
	bgCtx, stopBackground := context.WithCancel(ctx)
	defer stopBackground()

	var srv *server.Server
	upd := updater.New(
		func() updater.Config { return live.Get().Update.Updater() },
		func() string { return live.Get().DataDir },
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
	srv = server.New(live, *configPath, db, mgr, subs, upd, backups)

	go runLoop(bgCtx, live, "discover", func(c *config.Config) config.Duration { return c.DiscoverInterval }, 0, func(ctx context.Context) error {
		return mgr.DiscoverAll(ctx)
	})
	// Reconcile trails discovery so a freshly seen inbound can be provisioned in
	// the same cycle rather than the next one.
	go runLoop(bgCtx, live, "reconcile", func(c *config.Config) config.Duration { return c.SyncInterval }, 15*time.Second, func(ctx context.Context) error {
		return mgr.ReconcileAll(ctx)
	})
	go runLoop(bgCtx, live, "traffic", func(c *config.Config) config.Duration { return c.TrafficInterval }, 30*time.Second, func(ctx context.Context) error {
		return mgr.CollectTraffic(ctx)
	})
	go backups.Run(bgCtx)
	upd.StartBackground(bgCtx)

	// SIGHUP applies a hand-edited config file without a restart, the same
	// way saving it from the admin UI does.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := srv.ReloadConfig(); err != nil {
				log.Printf("firex: reload config: %v", err)
			}
		}
	}()

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
// fatal: one unreachable panel must not take the control plane down. The
// interval is re-read whenever the config changes, so a new one applies to the
// wait already in progress rather than only after it.
func runLoop(ctx context.Context, live *config.Live, name string, interval func(*config.Config) config.Duration, delay time.Duration, fn func(context.Context) error) {
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
	for {
		started := time.Now()
		runCtx, cancel := context.WithTimeout(ctx, interval(live.Get()).Duration())
		if err := fn(runCtx); err != nil {
			log.Printf("firex: %s: %v", name, err)
		}
		cancel()
		if !waitDue(ctx, live, started, interval) {
			return
		}
	}
}

// waitDue sleeps until one interval after started, measuring against the
// latest interval each time the config changes. It reports false once ctx ends.
func waitDue(ctx context.Context, live *config.Live, started time.Time, interval func(*config.Config) config.Duration) bool {
	for {
		// Taken before reading the interval, so a change landing in between
		// still wakes this wait.
		changed := live.Changed()
		wait := time.Until(started.Add(interval(live.Get()).Duration()))
		if wait <= 0 {
			return true
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			return true
		case <-changed:
			timer.Stop()
		case <-ctx.Done():
			timer.Stop()
			return false
		}
	}
}
