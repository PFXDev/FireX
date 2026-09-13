// Package config loads FireX's process settings from a JSON file.
//
// That file is the only source: nothing here reads the environment, so what an
// operator sees on disk is exactly what the process runs on. A missing file is
// written from the hardcoded template below, and a file left behind by an older
// build is completed in place, so config.json always lists every setting the
// running binary understands — including the ones that gained a default after
// it was last written.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PFXDev/FireX/internal/updater"
)

// DefaultPath keeps the config beside the database, so one directory carries a
// whole install and a volume mount needs no second entry.
const DefaultPath = "./data/config.json"

type Config struct {
	Listen  string `json:"listen"`
	DataDir string `json:"dataDir"`
	// DBPath overrides where the database lives. Empty follows DataDir, which
	// is why it is resolved after the file is written rather than before: the
	// file keeps the empty value and the derived path never freezes into it.
	DBPath     string `json:"dbPath"`
	SubBaseURL string `json:"subBaseUrl"`
	Debug      bool   `json:"debug"`

	// AdminUser names the admin: created on first start, and the account a
	// later AdminPassword override applies to.
	AdminUser string `json:"adminUser"`
	// AdminPassword is consumed, not kept. On first start it is the new
	// admin's password (empty means one is generated and printed once); on any
	// later start it resets that admin's password. Either way it is then
	// blanked out of the file, so the plaintext lives there only between the
	// operator writing it and the next start.
	AdminPassword string `json:"adminPassword"`

	SyncInterval     Duration `json:"syncInterval"`
	TrafficInterval  Duration `json:"trafficInterval"`
	DiscoverInterval Duration `json:"discoverInterval"`

	Update Update `json:"update"`
}

// Update drives self-updates from GitHub releases. Enabled only gates the
// periodic check; an admin can always trigger one from the UI.
type Update struct {
	Enabled       bool     `json:"enabled"`
	Channel       string   `json:"channel"`
	CheckInterval Duration `json:"checkInterval"`
	Source        string   `json:"source"`
	ProxyBaseURL  string   `json:"proxyBaseUrl"`
	Repo          string   `json:"repo"`
}

// Template is the config a fresh install starts from, and the fallback for every
// setting a hand-written file leaves out.
func Template() *Config {
	return &Config{
		Listen:           ":8080",
		DataDir:          "./data",
		DBPath:           "",
		SubBaseURL:       "",
		Debug:            false,
		AdminUser:        "admin",
		AdminPassword:    "",
		SyncInterval:     Duration(2 * time.Minute),
		TrafficInterval:  Duration(time.Minute),
		DiscoverInterval: Duration(5 * time.Minute),
		Update: Update{
			// Self-updating is opt-in: replacing the binary under a running
			// fleet is the operator's call, not a default.
			Enabled:       false,
			Channel:       "stable",
			CheckInterval: Duration(time.Hour),
			Source:        updater.SourceGitHub,
			ProxyBaseURL:  updater.DefaultProxyBaseURL,
			Repo:          updater.DefaultRepo,
		},
	}
}

// Load reads the config at path, creating it from Template when it is not there
// and completing it when it is missing settings or carries values FireX cannot
// act on. Only an unreadable or malformed file is an error: guessing at what a
// broken config meant would start the process on settings nobody chose.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	existed := true
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		existed, raw = false, nil
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Decoding onto the template rather than onto a zero value is what makes a
	// setting the file never mentions keep its default instead of arriving as
	// an empty string or a zero interval.
	cfg := Template()
	if existed {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	cfg.complete()

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", path, err)
	}
	out = append(out, '\n')
	if !bytes.Equal(out, raw) {
		switch err := write(path, out); {
		case err != nil:
			// The config in hand still describes this run correctly, so a
			// read-only config directory is worth a warning, not a refusal.
			log.Printf("firex: config: %v", err)
		case existed:
			log.Printf("firex: completed config %s", path)
		default:
			log.Printf("firex: wrote default config to %s", path)
		}
	}

	cfg.resolve()
	return cfg, nil
}

// Save writes an unresolved file config as it stands to path. Use Read for
// editing; Load derives runtime paths that must not be frozen into the file.
func (c *Config) Save(path string) error {
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return write(path, append(out, '\n'))
}

// complete fills in every setting the file left empty or set to something FireX
// cannot act on, so the file written back is the config this run actually uses
// rather than a list of intentions.
func (c *Config) complete() {
	def := Template()

	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = def.Listen
	}
	c.DataDir = strings.TrimSpace(c.DataDir)
	if c.DataDir == "" {
		c.DataDir = def.DataDir
	}
	c.DBPath = strings.TrimSpace(c.DBPath)
	// A trailing slash here would show up in the middle of every subscription
	// URL the UI hands out.
	c.SubBaseURL = strings.TrimRight(strings.TrimSpace(c.SubBaseURL), "/")
	c.AdminUser = strings.TrimSpace(c.AdminUser)
	if c.AdminUser == "" {
		c.AdminUser = def.AdminUser
	}

	for _, d := range []struct {
		value *Duration
		def   Duration
	}{
		{&c.SyncInterval, def.SyncInterval},
		{&c.TrafficInterval, def.TrafficInterval},
		{&c.DiscoverInterval, def.DiscoverInterval},
	} {
		if *d.value <= 0 {
			*d.value = d.def
		}
	}

	if c.Update.CheckInterval <= 0 {
		c.Update.CheckInterval = def.Update.CheckInterval
	}
	// The updater owns what a valid channel, source, mirror and repo are, so
	// its normalization decides here too rather than a second copy of the rules.
	settled := c.Update.Updater()
	c.Update.Channel = settled.Channel
	c.Update.Source = settled.Source
	c.Update.ProxyBaseURL = settled.ProxyBaseURL
	c.Update.Repo = settled.Repo
	c.Update.CheckInterval = Duration(time.Duration(settled.CheckInterval) * time.Second)
}

// resolve derives the settings that are deliberately absent from the file. It
// runs after the write so an empty dbPath stays empty on disk: moving dataDir
// then moves the database with it instead of stranding it.
func (c *Config) resolve() {
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, "firex.db")
	}
}

// Updater restates the update block in the shape the updater package takes,
// with the interval in the whole seconds it counts in.
func (u Update) Updater() updater.Config {
	return updater.Normalize(updater.Config{
		Enabled:       u.Enabled,
		Channel:       u.Channel,
		CheckInterval: int(time.Duration(u.CheckInterval).Seconds()),
		Source:        u.Source,
		ProxyBaseURL:  u.ProxyBaseURL,
		Repo:          u.Repo,
	})
}

// write replaces the config atomically. An admin password may be sitting in
// here, so the file is created fresh at 0600 rather than written through an
// existing mode, and a crash mid-write leaves the previous config intact.
func write(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".firex-config-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Duration is a time.Duration written in the config as a string such as "2m".
// JSON has no duration of its own, and a bare number of seconds reads worse in
// a file meant to be edited by hand.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%s: want a duration string such as \"2m\"", data)
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(text))
	if err != nil {
		return fmt.Errorf("%q: want a duration string such as \"2m\"", text)
	}
	*d = Duration(parsed)
	return nil
}
