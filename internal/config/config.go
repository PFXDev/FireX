// Package config reads FireX runtime settings from the environment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PFXDev/FireX/internal/updater"
)

type Config struct {
	Listen     string
	DBPath     string
	DataDir    string
	SubBaseURL string
	Debug      bool

	// Bootstrap admin, applied only when no admin row exists yet.
	AdminUser     string
	AdminPassword string

	SyncInterval     time.Duration
	TrafficInterval  time.Duration
	DiscoverInterval time.Duration

	// Update drives self-updates from GitHub releases. Enabled only gates the
	// periodic check; an admin can always trigger one from the UI.
	Update updater.Config
}

func Load() *Config {
	dataDir := env("FIREX_DATA_DIR", "./data")
	c := &Config{
		Listen:           env("FIREX_LISTEN", ":8080"),
		DataDir:          dataDir,
		DBPath:           env("FIREX_DB", filepath.Join(dataDir, "firex.db")),
		SubBaseURL:       strings.TrimRight(env("FIREX_SUB_BASE_URL", ""), "/"),
		Debug:            envBool("FIREX_DEBUG", false),
		AdminUser:        env("FIREX_ADMIN_USER", "admin"),
		AdminPassword:    os.Getenv("FIREX_ADMIN_PASSWORD"),
		SyncInterval:     envDuration("FIREX_SYNC_INTERVAL", 2*time.Minute),
		TrafficInterval:  envDuration("FIREX_TRAFFIC_INTERVAL", time.Minute),
		DiscoverInterval: envDuration("FIREX_DISCOVER_INTERVAL", 5*time.Minute),
		Update: updater.Config{
			// Self-updating is opt-in: replacing the binary under a running
			// fleet is the operator's call, not a default.
			Enabled:       envBool("FIREX_UPDATE_ENABLED", false),
			Channel:       env("FIREX_UPDATE_CHANNEL", "stable"),
			CheckInterval: int(envDuration("FIREX_UPDATE_INTERVAL", time.Hour).Seconds()),
			Source:        env("FIREX_UPDATE_SOURCE", updater.SourceGitHub),
			ProxyBaseURL:  env("FIREX_UPDATE_PROXY_BASE_URL", updater.DefaultProxyBaseURL),
			Repo:          env("FIREX_UPDATE_REPO", updater.DefaultRepo),
		},
	}
	// Settle the update block here so /api/version reports the same values the
	// updater acts on, rather than whatever casing the environment used.
	c.Update = updater.Normalize(c.Update)
	return c
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, err := strconv.ParseBool(env(key, ""))
	if err != nil {
		return def
	}
	return v
}

func envDuration(key string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(env(key, ""))
	if err != nil || d <= 0 {
		return def
	}
	return d
}
