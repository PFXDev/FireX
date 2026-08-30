package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/version"
)

// TestCheckOnlyAcrossSourcesAndChannels covers release selection over both
// sources (proxy mirror, GitHub direct) and both channels, including the
// dev-channel skip when the published commit is the one already running.
func TestCheckOnlyAcrossSourcesAndChannels(t *testing.T) {
	cases := []struct {
		name         string
		cfg          Config
		localVersion string
		localCommit  string
		remote       releaseVersionInfo
		direct       bool // serve the GitHub-direct layout instead of the mirror
		wantUpdate   bool
		wantLatest   string
	}{
		{
			name:         "proxy stable selects newer stable release",
			cfg:          Config{Channel: "stable", Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "v1.0.0",
			remote:       releaseVersionInfo{Tag: "v1.4.0"},
			wantUpdate:   true,
			wantLatest:   "v1.4.0",
		},
		{
			name:         "proxy stable ignores older stable release",
			cfg:          Config{Channel: "stable", Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "v1.4.0",
			remote:       releaseVersionInfo{Tag: "v1.3.9"},
			wantUpdate:   false,
			wantLatest:   "v1.3.9",
		},
		{
			name:         "proxy dev selects newer prerelease",
			cfg:          Config{Channel: "dev", Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "dev-0007-20260401-aaaaaaa",
			localCommit:  "aaaaaaa",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: "dev"},
			wantUpdate:   true,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "proxy dev skips release built from the running commit",
			cfg:          Config{Channel: "dev", Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "dev-0042-20260425-bbbbbbb",
			localCommit:  "bbbbbbb",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: "dev"},
			wantUpdate:   false,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "github direct dev reads the pinned dev tag",
			cfg:          Config{Channel: "dev", Source: SourceGitHub, Repo: "owner/repo"},
			localVersion: "dev-0007-20260401-aaaaaaa",
			localCommit:  "aaaaaaa",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: "dev"},
			direct:       true,
			wantUpdate:   true,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "github direct stable reads the latest redirect",
			cfg:          Config{Channel: "stable", Source: SourceGitHub, Repo: "owner/repo"},
			localVersion: "v1.0.0",
			remote:       releaseVersionInfo{Version: "v1.4.0", Commit: "bbbbbbb", Tag: "v1.4.0"},
			direct:       true,
			wantUpdate:   true,
			wantLatest:   "v1.4.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestVersion(t, tc.localVersion, tc.localCommit)
			cfg := tc.cfg

			if tc.direct {
				metadata, err := json.Marshal(tc.remote)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(routes(t, map[string]http.HandlerFunc{
					"/owner/repo/releases/download/" + tc.remote.Tag + "/version.json": writeBytes(metadata),
					"/owner/repo/releases/latest/download/version.json":                writeBytes(metadata),
				}))
				defer server.Close()
				setTestGitHubBaseURL(t, server.URL)
			} else {
				server := httptest.NewServer(routes(t, map[string]http.HandlerFunc{
					"/api/releases/owner/repo/latest": writeJSON(releaseInfo{TagName: tc.remote.Tag}),
					"/api/releases/owner/repo/dev": writeJSON(releaseInfo{
						TagName:    "dev",
						Prerelease: true,
						Assets: []assetInfo{{
							Name:               "version.json",
							BrowserDownloadURL: "https://github.com/owner/repo/releases/download/dev/version.json",
						}},
					}),
					"/download/owner/repo/dev/version.json": writeJSON(tc.remote),
				}))
				defer server.Close()
				cfg.ProxyBaseURL = server.URL
			}

			result, err := testUpdater(cfg).CheckOnly(context.Background())
			if err != nil {
				t.Fatalf("CheckOnly returned error: %v", err)
			}
			if result.HasUpdate != tc.wantUpdate || result.LatestVersion != tc.wantLatest {
				t.Fatalf("CheckOnly = %+v, want update=%v latest=%q", result, tc.wantUpdate, tc.wantLatest)
			}
		})
	}
}

func TestPerformUpdateDownloadsAndVerifiesPrerelease(t *testing.T) {
	setTestVersion(t, "dev-0007-20260401-aaaaaaa", "aaaaaaa")

	dataDir := t.TempDir()
	cfg := Config{Channel: "dev", Source: SourceProxy, Repo: "owner/repo"}
	u := New(func() Config { return cfg }, func() string { return dataDir }, discardLogger(), RestartHooks{})

	binary := []byte("new firex binary")
	target := u.targetName()
	// Several platforms in one manifest: the client has to select its line,
	// not just read the only one there is.
	sums := sha256SumsBody(map[string]string{
		"firex-linux-amd64":     strings.Repeat("11", 32),
		"firex-darwin-arm64":    strings.Repeat("22", 32),
		"firex-windows-amd64.e": strings.Repeat("33", 32),
		target:                  hashOf(binary),
	})

	server := httptest.NewServer(routes(t, map[string]http.HandlerFunc{
		"/api/releases/owner/repo/dev": writeJSON(releaseInfo{
			TagName:         "dev",
			TargetCommitish: "bbbbbbb",
			Prerelease:      true,
			Assets:          devAssets(target, int64(len(binary))),
		}),
		"/download/owner/repo/dev/" + target:        writeBytes(binary),
		"/download/owner/repo/dev/" + sumsAssetName: writeBytes([]byte(sums)),
		"/download/owner/repo/dev/version.json": writeJSON(releaseVersionInfo{
			Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: "dev",
		}),
	}))
	defer server.Close()
	cfg.ProxyBaseURL = server.URL

	u.performUpdate(context.Background())

	status := u.Status()
	if status.State != "ready" {
		t.Fatalf("state = %q (error %q), want ready", status.State, status.Error)
	}
	if status.LatestVersion != "dev-0042-20260425-bbbbbbb" || u.pendingTag != "dev" {
		t.Fatalf("latest=%q pendingTag=%q, want dev-0042-20260425-bbbbbbb / dev", status.LatestVersion, u.pendingTag)
	}
	if status.Progress != progressVerifyDone {
		t.Fatalf("progress = %.0f, want %d", status.Progress, progressVerifyDone)
	}
	got, err := os.ReadFile(u.pendingBinaryPath)
	if err != nil {
		t.Fatalf("read pending binary: %v", err)
	}
	if string(got) != string(binary) {
		t.Fatalf("pending binary = %q, want %q", got, binary)
	}
}

// TestPerformUpdateRejectsUnverifiableDownloads covers the three ways checksum
// verification can fail. Each one must abort the update: falling back to
// installing an unverified binary would make the whole check pointless.
func TestPerformUpdateRejectsUnverifiableDownloads(t *testing.T) {
	binary := []byte("new firex binary")

	cases := []struct {
		name      string
		omitSums  bool
		sums      func(target string) string
		wantError string
	}{
		{
			name:      "release without a checksum manifest",
			omitSums:  true,
			wantError: sumsAssetName,
		},
		{
			name: "manifest without an entry for this platform",
			sums: func(string) string {
				return sha256SumsBody(map[string]string{"firex-some-other-arch": strings.Repeat("aa", 32)})
			},
			wantError: "no sha256 entry",
		},
		{
			name: "manifest entry that does not match the bytes",
			sums: func(target string) string {
				return sha256SumsBody(map[string]string{target: strings.Repeat("bb", 32)})
			},
			wantError: "sha256 mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestVersion(t, "v1.0.0", "")

			dataDir := t.TempDir()
			cfg := Config{Channel: "stable", Source: SourceProxy, Repo: "owner/repo"}
			u := New(func() Config { return cfg }, func() string { return dataDir }, discardLogger(), RestartHooks{})

			target := u.targetName()
			assets := []assetInfo{{
				Name:               target,
				BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v1.4.0/" + target,
				Size:               int64(len(binary)),
			}}
			handlers := map[string]http.HandlerFunc{
				"/download/owner/repo/v1.4.0/" + target: writeBytes(binary),
			}
			if !tc.omitSums {
				assets = append(assets, assetInfo{
					Name:               sumsAssetName,
					BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v1.4.0/" + sumsAssetName,
				})
				handlers["/download/owner/repo/v1.4.0/"+sumsAssetName] = writeBytes([]byte(tc.sums(target)))
			}
			handlers["/api/releases/owner/repo/latest"] = writeJSON(releaseInfo{TagName: "v1.4.0", Assets: assets})

			server := httptest.NewServer(routes(t, handlers))
			defer server.Close()
			cfg.ProxyBaseURL = server.URL

			u.performUpdate(context.Background())

			status := u.Status()
			if status.State != "failed" {
				t.Fatalf("state = %q, want failed", status.State)
			}
			if !strings.Contains(status.Error, tc.wantError) {
				t.Fatalf("error = %q, want it to mention %q", status.Error, tc.wantError)
			}
			// Nothing unverified may survive in the updates directory.
			left, _ := filepath.Glob(filepath.Join(dataDir, "updates", "*"))
			if len(left) != 0 {
				t.Fatalf("update directory still holds %v", left)
			}
		})
	}
}

// TestSHA256ForTargetMatchesExactName guards the manifest parser against
// degrading to a prefix match, which would hand an arm binary to an arm64 host.
func TestSHA256ForTargetMatchesExactName(t *testing.T) {
	armSum := strings.Repeat("11", 32)
	arm64Sum := strings.Repeat("22", 32)
	sums := "" +
		armSum + "  firex-linux-arm\n" +
		arm64Sum + "  firex-linux-arm64\n" +
		"\n" +
		strings.Repeat("33", 32) + " *firex-windows-amd64.exe\n"

	for target, want := range map[string]string{
		"firex-linux-arm":         armSum,
		"firex-linux-arm64":       arm64Sum,
		"firex-windows-amd64.exe": strings.Repeat("33", 32),
	} {
		got, err := sha256ForTarget([]byte(sums), target)
		if err != nil {
			t.Fatalf("sha256ForTarget(%q): %v", target, err)
		}
		if got != want {
			t.Fatalf("sha256ForTarget(%q) = %s, want %s", target, got, want)
		}
	}

	if _, err := sha256ForTarget([]byte(sums), "firex-linux-riscv64"); err == nil {
		t.Fatal("expected an error for a target that is not listed")
	}
}

func TestApplyPendingMovesToApplyingBeforeAsyncRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	u := testUpdater(Config{})
	u.bgCtx = ctx
	// Fail the restart preparation so the test never reaches a real exec.
	u.hooks.BeforeExec = func(string) error { return context.Canceled }
	u.status.State = "ready"
	u.pendingBinaryPath = filepath.Join(t.TempDir(), "firex-new")
	u.pendingTag = "v1.2.0"

	if err := u.ApplyPending(context.Background()); err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	status := u.Status()
	if status.State != "applying" || status.Progress != progressApplying {
		t.Fatalf("status = %+v, want applying at %d", status, progressApplying)
	}
	if u.pendingBinaryPath != "" || u.pendingTag != "" {
		t.Fatal("expected the pending update to be consumed")
	}

	err := u.ApplyPending(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no pending update") {
		t.Fatalf("duplicate apply error = %v, want 'no pending update'", err)
	}

	time.Sleep(250 * time.Millisecond) // let the apply goroutine finish
}

func TestDismissPendingRemovesDownloadedBinary(t *testing.T) {
	u := testUpdater(Config{})
	pending := filepath.Join(t.TempDir(), "firex-dev")
	if err := os.WriteFile(pending, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	u.status.State = "ready"
	u.pendingBinaryPath = pending
	u.pendingTag = "dev"

	u.DismissPending()

	if state := u.Status().State; state != "idle" {
		t.Fatalf("state = %q, want idle", state)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending binary still present: %v", err)
	}
}

func TestWaitForIdleStopsWhenApplicationContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u := New(
		func() Config { return Config{} },
		func() string { return "" },
		discardLogger(),
		RestartHooks{IsBusy: func() bool { return true }},
	)

	if err := u.waitForIdle(ctx); err != context.Canceled {
		t.Fatalf("waitForIdle error = %v, want context.Canceled", err)
	}
}

func TestNormalizeConfigFillsDefaults(t *testing.T) {
	got := normalizeConfig(Config{Channel: "  DEV ", CheckInterval: -1, Source: "nonsense"})
	if got.Channel != "dev" || got.CheckInterval != 3600 || got.Source != SourceGitHub {
		t.Fatalf("normalizeConfig = %+v", got)
	}
	if got.Repo != DefaultRepo || got.ProxyBaseURL != DefaultProxyBaseURL {
		t.Fatalf("normalizeConfig = %+v, want the default repo and mirror", got)
	}
	if channel := normalizeConfig(Config{Channel: "anything-else"}).Channel; channel != "stable" {
		t.Fatalf("channel = %q, want stable", channel)
	}
}

// ------------------------------------------------------------------ fixtures

func devAssets(target string, size int64) []assetInfo {
	const base = "https://github.com/owner/repo/releases/download/dev/"
	return []assetInfo{
		{Name: target, BrowserDownloadURL: base + target, Size: size},
		{Name: sumsAssetName, BrowserDownloadURL: base + sumsAssetName},
		{Name: "version.json", BrowserDownloadURL: base + "version.json"},
	}
}

// sha256SumsBody renders the manifest the way `sha256sum` writes it: the hash,
// two spaces, then the bare file name.
func sha256SumsBody(entries map[string]string) string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s  %s\n", entries[name], name)
	}
	return b.String()
}

func hashOf(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// routes serves exactly the paths it is given. Anything else fails the test,
// which is what catches a client still asking for assets the release protocol
// no longer publishes.
func routes(t *testing.T, handlers map[string]http.HandlerFunc) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := handlers[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
}

func writeBytes(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }
}

func writeJSON(payload any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(payload) }
}

func setTestVersion(t *testing.T, ver, commit string) {
	t.Helper()
	originalVersion, originalCommit := version.Version, version.Commit
	version.Version = ver
	if commit != "" {
		version.Commit = commit
	}
	t.Cleanup(func() { version.Version, version.Commit = originalVersion, originalCommit })
}

func setTestGitHubBaseURL(t *testing.T, url string) {
	t.Helper()
	original := githubBaseURL
	githubBaseURL = url
	t.Cleanup(func() { githubBaseURL = original })
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func testUpdater(cfg Config) *Updater {
	return New(func() Config { return cfg }, func() string { return "" }, discardLogger(), RestartHooks{})
}
