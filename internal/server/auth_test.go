package server

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

// loadConfig writes contents as a config file and loads it the way main does,
// returning the config and where it lives.
func loadConfig(t *testing.T, contents string) (*config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return cfg, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// captureLog collects what BootstrapAdmin narrates, which is the operator's
// only view of a generated password.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func adminHash(t *testing.T, db *store.DB, username string) string {
	t.Helper()
	var admin model.Admin
	if err := db.First(&admin, "username = ?", username).Error; err != nil {
		t.Fatalf("admin %q: %v", username, err)
	}
	return admin.PasswordHash
}

func TestBootstrapFirstStartGeneratesAndPrintsOnce(t *testing.T) {
	db := openDB(t)
	cfg, path := loadConfig(t, `{"adminUser": "root"}`)
	logs := captureLog(t)

	if err := BootstrapAdmin(db, cfg, path); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	match := regexp.MustCompile(`generated password: (\S+)`).FindStringSubmatch(logs.String())
	if match == nil {
		t.Fatalf("generated password was not printed; log:\n%s", logs)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(adminHash(t, db, "root")), []byte(match[1])); err != nil {
		t.Errorf("printed password does not open the admin: %v", err)
	}
	// Nothing to consume, so nothing to write: the setting stays empty and the
	// generated password is nowhere but the log.
	if file := readFile(t, path); !strings.Contains(file, `"adminPassword": ""`) || strings.Contains(file, match[1]) {
		t.Errorf("config after a generated password:\n%s", file)
	}
}

func TestBootstrapFirstStartConsumesPassword(t *testing.T) {
	db := openDB(t)
	cfg, path := loadConfig(t, `{"adminPassword": "hunter2-hunter2"}`)
	captureLog(t)

	if err := BootstrapAdmin(db, cfg, path); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	hash := adminHash(t, db, "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("hunter2-hunter2")); err != nil {
		t.Errorf("admin was not created with the configured password: %v", err)
	}
	if file := readFile(t, path); strings.Contains(file, "hunter2") {
		t.Errorf("consumed password is still in the file:\n%s", file)
	}
	if cfg.AdminPassword != "" {
		t.Errorf("cfg.AdminPassword = %q after consumption, want empty", cfg.AdminPassword)
	}

	// The next start reads the blanked file and leaves the admin alone.
	again, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() on the blanked file: %v", err)
	}
	if err := BootstrapAdmin(db, again, path); err != nil {
		t.Fatalf("second BootstrapAdmin() error = %v", err)
	}
	if adminHash(t, db, "admin") != hash {
		t.Error("a start with an empty adminPassword changed the admin")
	}
}

// TestBootstrapOverrideResetsPassword is the lost-password path: write a
// password, restart, and whoever was signed in under the old one is out.
func TestBootstrapOverrideResetsPassword(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.mustDo(http.MethodGet, "/api/auth/me", nil)
	captureLog(t)

	cfg, path := loadConfig(t, `{"adminPassword": "reset-me-please"}`)
	if err := BootstrapAdmin(h.db, cfg, path); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}

	resp, _ := h.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /auth/me after the reset = %d, want 401", resp.StatusCode)
	}
	resp, _ = h.do(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin", "password": "password123",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with the old password = %d, want 401", resp.StatusCode)
	}
	h.mustDo(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin", "password": "reset-me-please",
	})
	if file := readFile(t, path); strings.Contains(file, "reset-me-please") {
		t.Errorf("consumed password is still in the file:\n%s", file)
	}
}

func TestBootstrapOverrideNeedsTheNamedAdmin(t *testing.T) {
	h := newHarness(t)
	before := adminHash(t, h.db, "admin")
	logs := captureLog(t)

	cfg, path := loadConfig(t, `{"adminUser": "root", "adminPassword": "reset-me-please"}`)
	if err := BootstrapAdmin(h.db, cfg, path); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	if adminHash(t, h.db, "admin") != before {
		t.Error("a password meant for \"root\" was applied to \"admin\"")
	}
	if !strings.Contains(logs.String(), `the admin is "admin"`) {
		t.Errorf("log should name the admin that exists, got:\n%s", logs)
	}
	// Kept, so that fixing adminUser and restarting is the whole repair.
	if file := readFile(t, path); !strings.Contains(file, `"adminPassword": "reset-me-please"`) {
		t.Errorf("an unapplied password was blanked:\n%s", file)
	}
	h.login()
}

func TestBootstrapAppliesEvenWhenItCannotBlank(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory modes do not bind root")
	}
	h := newHarness(t)
	logs := captureLog(t)

	cfg, path := loadConfig(t, `{"adminPassword": "reset-me-please"}`)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod config dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := BootstrapAdmin(h.db, cfg, path); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	// The reset itself is what the operator asked for; the warning is about
	// the copy left behind.
	h.mustDo(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin", "password": "reset-me-please",
	})
	if !strings.Contains(logs.String(), "could not blank adminPassword") {
		t.Errorf("no warning about the password left in the file, got:\n%s", logs)
	}
}
