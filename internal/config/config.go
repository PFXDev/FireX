// Package config reads FireX runtime settings from the environment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	}
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
