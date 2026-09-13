package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/PFXDev/FireX/internal/config"
)

type configResponse struct {
	Config                  config.Config `json:"config"`
	Path                    string        `json:"path"`
	Revision                string        `json:"revision"`
	RestartRequired         bool          `json:"restartRequired"`
	HasPendingAdminPassword bool          `json:"hasPendingAdminPassword"`
}

func (h *harness) serverConfig() configResponse {
	h.t.Helper()
	var out configResponse
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/settings/server", nil), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func TestServerConfigRequiresAdmin(t *testing.T) {
	h := newHarness(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		resp, _ := h.do(method, "/api/settings/server", map[string]any{})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", method, resp.StatusCode)
		}
	}
}

func TestServerConfigPersistsToCustomPathWithoutChangingRuntime(t *testing.T) {
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	if before.Path != h.configPath || before.Config.DBPath != "" || before.RestartRequired {
		t.Fatalf("initial file view is incorrect: %+v", before)
	}
	patch := map[string]any{
		"listen": "127.0.0.1:9090", "dataDir": "./moved-data", "debug": true,
		"subBaseUrl": " https://sub.example.com/ ", "syncInterval": "90s",
		"trafficInterval": "30s", "discoverInterval": "10m",
		"update": map[string]any{"enabled": true, "channel": "dev", "source": "proxy", "checkInterval": "2h", "repo": "example/FireX", "proxyBaseUrl": "https://mirror.example.com"},
	}
	h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{"revision": before.Revision, "config": patch})
	after := h.serverConfig()
	if !after.RestartRequired || after.Config.SubBaseURL != "https://sub.example.com" || after.Config.DBPath != "" {
		t.Fatalf("saved file view is incorrect: %+v", after)
	}
	if h.cfg.Listen != ":8080" || h.cfg.Debug || h.cfg.Update.Enabled {
		t.Fatal("save mutated running settings")
	}
	loaded, err := config.Load(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Listen != "127.0.0.1:9090" || loaded.DBPath != "moved-data/firex.db" || loaded.Update.Repo != "example/FireX" || loaded.SyncInterval != config.Duration(90_000_000_000) {
		t.Fatalf("next startup does not load the edit: %+v", loaded)
	}
	info, err := os.Stat(h.configPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config must remain private: %v, %v", info, err)
	}
	// Restoring the startup values clears the pending flag without a restart.
	h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{"revision": after.Revision, "config": before.Config})
	if h.serverConfig().RestartRequired {
		t.Fatal("restored startup settings still require restart")
	}
}

func TestServerConfigPasswordIsWriteOnlyAndConsumedOnRestart(t *testing.T) {
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	const password = "pending-reset-123"
	raw := h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{
		"revision": before.Revision, "config": map[string]any{"adminPassword": password},
	})
	if strings.Contains(string(raw), password) {
		t.Fatal("save response leaked password")
	}
	after := h.serverConfig()
	if after.Config.AdminPassword != "" || !after.HasPendingAdminPassword || !after.RestartRequired {
		t.Fatalf("password not redacted or not marked pending: %+v", after)
	}
	h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{"revision": after.Revision, "config": map[string]any{"debug": true}})
	if !strings.Contains(readFile(t, h.configPath), password) {
		t.Fatal("unrelated edit cleared pending password")
	}
	h.mustDo(http.MethodGet, "/api/auth/me", nil)
	loaded, err := config.Load(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := BootstrapAdmin(h.db, loaded, h.configPath); err != nil {
		t.Fatal(err)
	}
	file, err := config.Read(h.configPath)
	if err != nil || file.AdminPassword != "" || file.DBPath != "" {
		t.Fatalf("bootstrap did not consume password or froze dbPath: %+v, %v", file, err)
	}
	if resp, _ := h.do(http.MethodGet, "/api/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("restart reset did not revoke old session")
	}
	h.mustDo(http.MethodPost, "/api/auth/login", map[string]string{"username": "admin", "password": password})
}

func TestServerConfigCanCancelPasswordReset(t *testing.T) {
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{"revision": before.Revision, "config": map[string]any{"adminPassword": "pending-reset-123"}})
	after := h.serverConfig()
	h.mustDo(http.MethodPut, "/api/settings/server", map[string]any{"revision": after.Revision, "config": map[string]any{"adminPassword": ""}})
	if got := h.serverConfig(); got.HasPendingAdminPassword || got.RestartRequired {
		t.Fatalf("reset was not cancelled: %+v", got)
	}
}

func TestServerConfigRejectsInvalidEditsWithoutWriting(t *testing.T) {
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	file := readFile(t, h.configPath)
	for _, patch := range []map[string]any{
		{"listen": "bad-address"}, {"listen": ":70000"}, {"dataDir": " "},
		{"syncInterval": "0s"}, {"trafficInterval": "-1m"}, {"discoverInterval": "oops"}, {"syncInterval": 120},
		{"subBaseUrl": "javascript:alert(1)"}, {"adminUser": ""}, {"adminPassword": "short"},
		{"adminUser": "missing", "adminPassword": "pending-reset-123"},
		{"update": map[string]any{"channel": "nightly"}},
		{"update": map[string]any{"source": "invalid"}},
		{"update": map[string]any{"checkInterval": "30s"}},
		{"update": map[string]any{"repo": "../repo"}},
		{"update": map[string]any{"proxyBaseUrl": "file:///tmp/update"}},
		{"unknown": true}, {"update": map[string]any{"unknown": true}},
	} {
		resp, body := h.do(http.MethodPut, "/api/settings/server", map[string]any{"revision": before.Revision, "config": patch})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("patch %v = %d: %s", patch, resp.StatusCode, body)
		}
		if readFile(t, h.configPath) != file {
			t.Fatalf("rejected edit %v changed config", patch)
		}
	}
}

func TestServerConfigDetectsExternalAndConcurrentEdits(t *testing.T) {
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	external := before.Config
	external.Listen = ":8181"
	if err := external.Save(h.configPath); err != nil {
		t.Fatal(err)
	}
	resp, _ := h.do(http.MethodPut, "/api/settings/server", map[string]any{"revision": before.Revision, "config": map[string]any{"debug": true}})
	if resp.StatusCode != http.StatusConflict || h.serverConfig().Config.Listen != ":8181" {
		t.Fatal("stale edit overwrote external changes")
	}
	latest := h.serverConfig()
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, listen := range []string{":8282", ":8383"} {
		wg.Go(func() {
			resp, _ := h.do(http.MethodPut, "/api/settings/server", map[string]any{"revision": latest.Revision, "config": map[string]any{"listen": listen}})
			codes <- resp.StatusCode
		})
	}
	wg.Wait()
	a, b := <-codes, <-codes
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("concurrent saves = %d, %d; want one success and one conflict", a, b)
	}
}

func TestServerConfigWriteFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory modes do not bind root")
	}
	h := newHarness(t)
	h.login()
	before := h.serverConfig()
	file := readFile(t, h.configPath)
	dir := strings.TrimSuffix(h.configPath, "/custom-config.json")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	resp, _ := h.do(http.MethodPut, "/api/settings/server", map[string]any{"revision": before.Revision, "config": map[string]any{"debug": true}})
	if resp.StatusCode != http.StatusInternalServerError || readFile(t, h.configPath) != file {
		t.Fatal("failed save did not preserve the original file and report failure")
	}
}
