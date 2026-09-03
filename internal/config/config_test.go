package config

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/updater"
)

func TestMain(m *testing.M) {
	// Load narrates what it wrote; the tests assert on the files instead.
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// load runs Load against a fresh directory, optionally seeded with a config
// file, and returns the config plus the bytes left on disk.
func load(t *testing.T, contents string) (*Config, string, string) {
	t.Helper()
	// A nested directory also covers the case where the config's parent does
	// not exist yet, which is every first start.
	path := filepath.Join(t.TempDir(), "firex", "config.json")
	if contents != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("prepare config dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	return cfg, string(written), path
}

func TestLoadReleasesTemplate(t *testing.T) {
	cfg, written, path := load(t, "")

	want := Template()
	want.resolve()
	if *cfg != *want {
		t.Fatalf("Load() = %+v, want %+v", cfg, want)
	}

	// The bootstrap password lives in this file, so it must not be readable by
	// anything but the account FireX runs as.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config mode = %o, want 600", mode)
	}

	var file Config
	if err := json.Unmarshal([]byte(written), &file); err != nil {
		t.Fatalf("released template is not valid JSON: %v", err)
	}
	// dbPath is derived, not stored: freezing it into the file would strand the
	// database when dataDir moves.
	if file.DBPath != "" {
		t.Errorf("released dbPath = %q, want empty", file.DBPath)
	}
	if file.SyncInterval != Template().SyncInterval {
		t.Errorf("released syncInterval = %v, want %v", file.SyncInterval, Template().SyncInterval)
	}
	if !strings.Contains(written, `"syncInterval": "2m0s"`) {
		t.Errorf("durations should be written as strings, got:\n%s", written)
	}
}

func TestLoadCompletesMissingSettings(t *testing.T) {
	cfg, written, _ := load(t, `{"listen": ":9000", "adminPassword": "hunter2"}`)

	if cfg.Listen != ":9000" {
		t.Errorf("Listen = %q, want :9000", cfg.Listen)
	}
	if cfg.AdminPassword != "hunter2" {
		t.Errorf("AdminPassword = %q, want hunter2", cfg.AdminPassword)
	}
	if cfg.SyncInterval != Template().SyncInterval {
		t.Errorf("SyncInterval = %v, want the default %v", cfg.SyncInterval, Template().SyncInterval)
	}
	if cfg.AdminUser != "admin" {
		t.Errorf("AdminUser = %q, want admin", cfg.AdminUser)
	}
	if cfg.Update.Repo != updater.DefaultRepo {
		t.Errorf("Update.Repo = %q, want %q", cfg.Update.Repo, updater.DefaultRepo)
	}

	// The point of completing is that the operator can now see and edit every
	// setting, not just the ones they happened to write.
	for _, key := range []string{"dataDir", "discoverInterval", "update", "proxyBaseUrl"} {
		if !strings.Contains(written, `"`+key+`"`) {
			t.Errorf("completed config is missing %q:\n%s", key, written)
		}
	}
	if !strings.Contains(written, `"adminPassword": "hunter2"`) {
		t.Errorf("completing dropped a setting the operator wrote:\n%s", written)
	}
}

func TestLoadReplacesUnusableValues(t *testing.T) {
	cfg, written, _ := load(t, `{
	  "listen": "   ",
	  "syncInterval": "0s",
	  "trafficInterval": "-1m",
	  "subBaseUrl": "https://sub.example.com/",
	  "update": {"channel": "DEV", "source": "nowhere", "proxyBaseUrl": "https://mirror.example.com/", "checkInterval": "0s"}
	}`)

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want the default :8080", cfg.Listen)
	}
	if cfg.SyncInterval != Duration(2*time.Minute) {
		t.Errorf("SyncInterval = %v, want 2m", cfg.SyncInterval)
	}
	if cfg.TrafficInterval != Duration(time.Minute) {
		t.Errorf("TrafficInterval = %v, want 1m", cfg.TrafficInterval)
	}
	if cfg.SubBaseURL != "https://sub.example.com" {
		t.Errorf("SubBaseURL = %q, want the trailing slash gone", cfg.SubBaseURL)
	}
	if cfg.Update.Channel != "dev" {
		t.Errorf("Update.Channel = %q, want dev", cfg.Update.Channel)
	}
	if cfg.Update.Source != updater.SourceGitHub {
		t.Errorf("Update.Source = %q, want %q for an unknown source", cfg.Update.Source, updater.SourceGitHub)
	}
	if cfg.Update.ProxyBaseURL != "https://mirror.example.com" {
		t.Errorf("Update.ProxyBaseURL = %q, want the trailing slash gone", cfg.Update.ProxyBaseURL)
	}
	if cfg.Update.CheckInterval != Duration(time.Hour) {
		t.Errorf("Update.CheckInterval = %v, want the default 1h", cfg.Update.CheckInterval)
	}

	// What was replaced is written back, so the file explains the run.
	if strings.Contains(written, `"DEV"`) || strings.Contains(written, `"nowhere"`) {
		t.Errorf("unusable values survived into the file:\n%s", written)
	}
}

func TestLoadDerivesDBPathFromDataDir(t *testing.T) {
	cfg, written, _ := load(t, `{"dataDir": "/var/lib/firex"}`)
	if want := filepath.Join("/var/lib/firex", "firex.db"); cfg.DBPath != want {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, want)
	}
	if strings.Contains(written, "firex.db") {
		t.Errorf("derived dbPath leaked into the file:\n%s", written)
	}

	explicit, _, _ := load(t, `{"dataDir": "/var/lib/firex", "dbPath": "/srv/firex.db"}`)
	if explicit.DBPath != "/srv/firex.db" {
		t.Errorf("DBPath = %q, want the configured /srv/firex.db", explicit.DBPath)
	}
}

// TestLoadLeavesCompleteFileAlone guards the rewrite path: a file that already
// says everything must survive a restart byte for byte, or config management
// would see FireX fighting it on every start.
func TestLoadLeavesCompleteFileAlone(t *testing.T) {
	_, first, path := load(t, "")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	// A rewrite would land in the same second, so compare the bytes and the
	// modification time together rather than sleeping a second for the mtime.
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	if string(second) != first {
		t.Errorf("second Load() rewrote the config:\n%s\nwant:\n%s", second, first)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("second Load() touched a config that needed no change")
	}
}

func TestLoadRejectsBrokenFile(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		wantMsg  string
	}{
		{name: "malformed json", contents: `{"listen": ":9000"`, wantMsg: "parse"},
		{name: "duration as a number", contents: `{"syncInterval": 120}`, wantMsg: "duration string"},
		{name: "unparsable duration", contents: `{"syncInterval": "2 minutes"}`, wantMsg: "duration string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want a failure rather than silent defaults")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("Load() error = %v, want it to mention %q", err, tc.wantMsg)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("Load() error = %v, want it to name %s", err, path)
			}
			// A broken config is never overwritten: the operator's file is the
			// only copy of what they meant.
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back config: %v", err)
			}
			if string(after) != tc.contents {
				t.Errorf("a rejected config was rewritten to:\n%s", after)
			}
		})
	}
}

func TestUpdaterConvertsInterval(t *testing.T) {
	cfg := Template()
	cfg.Update.CheckInterval = Duration(90 * time.Minute)
	if got := cfg.Update.Updater().CheckInterval; got != 5400 {
		t.Errorf("CheckInterval = %d seconds, want 5400", got)
	}
}
