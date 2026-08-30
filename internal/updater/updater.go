// Package updater keeps a running FireX binary current with the GitHub
// releases produced by .github/workflows/cross-compile.yml.
//
// Integrity rests on one layer: every release ships a single SHA256SUMS
// listing all of its binaries, and a download that cannot be matched against
// its entry is refused rather than installed. There is deliberately no release
// signature, so a compromised download channel could serve a poisoned binary
// with a matching checksum; the mitigation is HTTPS plus defaulting to GitHub
// directly instead of a mirror.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PFXDev/FireX/internal/version"
)

type Config struct {
	Enabled       bool   `json:"enabled"`
	Channel       string `json:"channel"`
	CheckInterval int    `json:"check_interval"`
	Source        string `json:"source"`
	ProxyBaseURL  string `json:"proxy_base_url"`
	Repo          string `json:"repo"`
}

type Status struct {
	State            string  `json:"state"`
	CurrentVersion   string  `json:"current_version"`
	LatestVersion    string  `json:"latest_version,omitempty"`
	IsPrerelease     bool    `json:"is_prerelease"`
	Progress         float64 `json:"progress,omitempty"`
	DownloadProgress float64 `json:"download_progress,omitempty"`
	Error            string  `json:"error,omitempty"`
	LastCheck        string  `json:"last_check,omitempty"`
	ReleaseNotes     string  `json:"release_notes,omitempty"`
}

type CheckResult struct {
	HasUpdate      bool   `json:"has_update"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	IsPrerelease   bool   `json:"is_prerelease"`
	ReleaseNotes   string `json:"release_notes,omitempty"`
	Channel        string `json:"channel"`
}

type RestartHooks struct {
	// BeforeExec releases everything the successor process needs to take
	// over: the listening socket and the database handle. Returning an error
	// aborts the update instead of restarting into a broken state.
	BeforeExec func(tag string) error
	// OnExecFailure reports a restart that never happened, leaving the
	// process alive but already torn down.
	OnExecFailure func(error)
	// IsBusy reports work that a restart would interrupt. The updater waits
	// for it to go false, bounded by idleWaitTimeout, before applying.
	IsBusy func() bool
}

type Updater struct {
	cfg     func() Config
	dataDir func() string
	logger  *log.Logger
	hooks   RestartHooks

	mu     sync.RWMutex
	status Status

	bgCtx context.Context

	pendingBinaryPath string
	pendingTag        string
}

const (
	idleWaitTimeout  = 10 * time.Minute
	idlePollInterval = 5 * time.Second
	downloadTimeout  = 30 * time.Minute

	// SourceGitHub reads release assets from github.com/<repo>/releases/...
	// download links. Those are CDN redirects rather than REST API calls, so
	// they carry no rate limit.
	SourceGitHub = "github"
	// SourceProxy routes both lookups and downloads through the configured
	// proxy_base_url mirror.
	SourceProxy = "proxy"

	// DefaultRepo is the release source for official builds.
	DefaultRepo = "PFXDev/FireX"
	// DefaultProxyBaseURL is the fallback mirror for the proxy source.
	DefaultProxyBaseURL = "https://dl.repo.chycloud.top"

	// sumsAssetName is the one checksum manifest each release carries; it
	// covers every platform binary in that release.
	sumsAssetName = "SHA256SUMS"

	progressChecking      = 5
	progressReleaseFound  = 10
	progressDownloadStart = 10
	progressDownloadDone  = 90
	progressVerifyStart   = 92
	progressVerifyDone    = 95
	progressApplying      = 98
	progressComplete      = 100
)

func New(cfg func() Config, dataDir func() string, logger *log.Logger, hooks RestartHooks) *Updater {
	if logger == nil {
		logger = log.Default()
	}
	return &Updater{
		cfg:     cfg,
		dataDir: dataDir,
		logger:  logger,
		hooks:   hooks,
		status: Status{
			State:          "idle",
			CurrentVersion: version.Version,
		},
	}
}

func (u *Updater) Status() Status {
	u.mu.RLock()
	defer u.mu.RUnlock()
	s := u.status
	s.CurrentVersion = version.Version
	return s
}

// CheckOnly looks for a newer release without downloading anything.
func (u *Updater) CheckOnly(ctx context.Context) (CheckResult, error) {
	cfg := normalizeConfig(u.cfg())
	result := CheckResult{
		CurrentVersion: version.Version,
		Channel:        cfg.Channel,
	}

	release, hasUpdate, err := u.checkForUpdate(ctx, cfg)
	if err != nil {
		return result, err
	}

	u.mu.Lock()
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
	u.mu.Unlock()

	if release == nil {
		return result, nil
	}

	result.HasUpdate = hasUpdate
	result.LatestVersion = release.displayVersion()
	result.IsPrerelease = release.Prerelease
	result.ReleaseNotes = release.Body

	u.mu.Lock()
	u.status.LatestVersion = release.displayVersion()
	u.status.IsPrerelease = release.Prerelease
	u.status.ReleaseNotes = release.Body
	u.mu.Unlock()

	return result, nil
}

// StartUpdate runs a full check-download-apply pass in the background. The
// caller's context is deliberately ignored: an HTTP request context dies with
// its response and would abandon the download halfway.
func (u *Updater) StartUpdate(_ context.Context) {
	go u.performUpdate(u.bgContext())
}

// ApplyPending installs the pre-release a previous pass parked in "ready".
func (u *Updater) ApplyPending(_ context.Context) error {
	u.mu.Lock()
	state := u.status.State
	path := u.pendingBinaryPath
	tag := u.pendingTag

	if state != "ready" || path == "" {
		u.mu.Unlock()
		return fmt.Errorf("no pending update to apply")
	}

	// Claim the pending update synchronously so a second call is rejected and
	// the next status poll already reads "applying".
	u.status.State = "applying"
	u.status.Progress = progressApplying
	u.status.DownloadProgress = 0
	u.pendingBinaryPath = ""
	u.pendingTag = ""
	u.mu.Unlock()

	go func() {
		time.Sleep(200 * time.Millisecond)
		if err := u.waitForIdle(u.bgContext()); err != nil {
			u.setError("apply canceled while waiting for idle: " + err.Error())
			return
		}
		if err := u.applyUpdate(path, tag); err != nil {
			u.notifyExecFailure(err)
			u.setError("apply failed: " + err.Error())
		}
	}()
	return nil
}

// DismissPending throws away a downloaded pre-release.
func (u *Updater) DismissPending() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status.State == "ready" {
		if u.pendingBinaryPath != "" {
			_ = os.Remove(u.pendingBinaryPath)
		}
		u.pendingBinaryPath = ""
		u.pendingTag = ""
		u.status.State = "idle"
		u.status.LatestVersion = ""
		u.status.Progress = 0
		u.status.DownloadProgress = 0
		u.status.Error = ""
	}
}

// StartBackground begins periodic checks. ctx also becomes the context for
// manually triggered updates, which outlive the request that started them.
func (u *Updater) StartBackground(ctx context.Context) {
	u.bgCtx = ctx
	cfg := normalizeConfig(u.cfg())
	if !cfg.Enabled {
		u.logger.Printf("firex: automatic updates disabled")
		return
	}
	u.logger.Printf("firex: automatic updates enabled, channel=%s source=%s interval=%ds", cfg.Channel, cfg.Source, cfg.CheckInterval)
	go u.loop(ctx)
}

func (u *Updater) bgContext() context.Context {
	if u.bgCtx != nil {
		return u.bgCtx
	}
	return context.Background()
}

func (u *Updater) loop(ctx context.Context) {
	// Let the panels settle before spending bandwidth on a version check.
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		return
	}

	u.checkAndUpdate(ctx)

	for {
		cfg := normalizeConfig(u.cfg())
		interval := time.Duration(cfg.CheckInterval) * time.Second
		if interval < time.Minute {
			interval = time.Minute
		}
		select {
		case <-time.After(interval):
			u.checkAndUpdate(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (u *Updater) checkAndUpdate(ctx context.Context) {
	cfg := normalizeConfig(u.cfg())
	if !cfg.Enabled {
		return
	}
	u.performUpdate(ctx)
}

func (u *Updater) performUpdate(ctx context.Context) {
	cfg := normalizeConfig(u.cfg())

	u.mu.Lock()
	if u.status.State == "checking" || u.status.State == "ready" || u.status.State == "downloading" || u.status.State == "applying" {
		u.mu.Unlock()
		return
	}
	u.status.State = "checking"
	u.status.Progress = progressChecking
	u.status.Error = ""
	u.status.DownloadProgress = 0
	u.mu.Unlock()

	release, hasUpdate, err := u.checkForUpdate(ctx, cfg)
	if err != nil {
		u.setError("check failed: " + err.Error())
		return
	}
	if release == nil || !hasUpdate {
		u.mu.Lock()
		u.status.State = "idle"
		u.status.Progress = 0
		u.status.DownloadProgress = 0
		u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
		u.mu.Unlock()
		return
	}

	u.mu.Lock()
	u.status.LatestVersion = release.displayVersion()
	u.status.IsPrerelease = release.Prerelease
	u.status.ReleaseNotes = release.Body
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
	u.status.Progress = progressReleaseFound
	u.mu.Unlock()

	binaryPath, err := u.download(ctx, cfg, release)
	if err != nil {
		u.setError("download failed: " + err.Error())
		return
	}

	if cfg.Channel == "stable" {
		u.mu.Lock()
		u.status.State = "applying"
		u.status.Progress = progressApplying
		u.status.DownloadProgress = 0
		u.mu.Unlock()
		if err := u.waitForIdle(ctx); err != nil {
			u.setError("apply canceled while waiting for idle: " + err.Error())
			return
		}
		if err := u.applyUpdate(binaryPath, release.TagName); err != nil {
			u.notifyExecFailure(err)
			u.setError("apply failed: " + err.Error())
		}
		return
	}

	// Dev builds are not installed unattended: an admin confirms the restart.
	u.mu.Lock()
	u.status.State = "ready"
	u.status.Progress = progressVerifyDone
	u.status.DownloadProgress = 0
	u.pendingBinaryPath = binaryPath
	u.pendingTag = release.TagName
	u.mu.Unlock()
	u.logger.Printf("firex: pre-release %s ready, waiting for admin confirmation", release.TagName)
}

func (u *Updater) setError(msg string) {
	u.logger.Printf("firex: update: %s", msg)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.State = "failed"
	u.status.Error = msg
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
}

func clampProgress(progress float64) float64 {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func overallDownloadProgress(downloadProgress float64) float64 {
	downloadProgress = clampProgress(downloadProgress)
	span := progressDownloadDone - progressDownloadStart
	return progressDownloadStart + downloadProgress*float64(span)/100
}

func (u *Updater) notifyExecFailure(err error) {
	if err == nil || u.hooks.OnExecFailure == nil {
		return
	}
	u.hooks.OnExecFailure(err)
}

// waitForIdle blocks until the host reports no in-flight work. A canceled
// context aborts the update; only the deliberate timeout allows a restart
// while work is still reported as active.
func (u *Updater) waitForIdle(ctx context.Context) error {
	if u.hooks.IsBusy == nil || !u.hooks.IsBusy() {
		return nil
	}
	u.logger.Printf("firex: waiting for in-flight panel work before restarting (max %s)", idleWaitTimeout)
	deadline := time.After(idleWaitTimeout)
	ticker := time.NewTicker(idlePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			u.logger.Printf("firex: idle wait timed out, restarting anyway")
			return nil
		case <-ticker.C:
			if !u.hooks.IsBusy() {
				return nil
			}
		}
	}
}

type releaseInfo struct {
	TagName         string      `json:"tag_name"`
	TargetCommitish string      `json:"target_commitish"`
	Prerelease      bool        `json:"prerelease"`
	Body            string      `json:"body"`
	Assets          []assetInfo `json:"assets"`
	Version         string
	Commit          string
	BuildTime       string
}

type assetInfo struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type releaseVersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Tag       string `json:"tag"`
}

func (r releaseInfo) displayVersion() string {
	if strings.TrimSpace(r.Version) != "" {
		return strings.TrimSpace(r.Version)
	}
	return r.TagName
}

// githubBaseURL is a var so tests can point direct-source checks at a local
// server.
var githubBaseURL = "https://github.com"

func (u *Updater) checkForUpdate(ctx context.Context, cfg Config) (*releaseInfo, bool, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var (
		release *releaseInfo
		err     error
	)
	if cfg.Source == SourceProxy {
		release, err = u.fetchReleaseViaProxy(checkCtx, cfg)
	} else {
		release, err = u.fetchReleaseFromGitHub(checkCtx, cfg)
	}
	if err != nil || release == nil {
		return nil, false, err
	}
	if !u.isNewer(*release, cfg.Channel) {
		u.logger.Printf("firex: already up to date (%s)", release.displayVersion())
		return release, false, nil
	}
	return release, true, nil
}

// fetchReleaseFromGitHub resolves a release without touching the REST API: it
// reads version.json straight off the release download URL (the pinned "dev"
// tag, or the "latest" redirect for stable) and synthesizes the asset list
// from the tag that file names. version.json is unauthenticated metadata that
// only decides which tag to pull from; the checksum check on the downloaded
// binary is what actually gates installation.
func (u *Updater) fetchReleaseFromGitHub(ctx context.Context, cfg Config) (*releaseInfo, error) {
	base := githubBaseURL + "/" + cfg.Repo + "/releases"
	versionURL := base + "/latest/download/version.json"
	if cfg.Channel != "stable" {
		versionURL = base + "/download/dev/version.json"
	}
	u.logger.Printf("firex: checking %s", versionURL)

	body, status, err := u.httpGet(ctx, versionURL, 16*1024)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		u.logger.Printf("firex: no release found for channel %s", cfg.Channel)
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", status)
	}

	var info releaseVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode version metadata: %w", err)
	}
	tag := strings.TrimSpace(info.Tag)
	if cfg.Channel != "stable" {
		tag = "dev"
	} else if tag == "" {
		return nil, fmt.Errorf("version metadata missing release tag")
	}

	assetNames := []string{u.targetName(), sumsAssetName, "version.json"}
	assets := make([]assetInfo, 0, len(assetNames))
	for _, name := range assetNames {
		assets = append(assets, assetInfo{
			Name:               name,
			BrowserDownloadURL: base + "/download/" + tag + "/" + name,
		})
	}
	return &releaseInfo{
		TagName:    tag,
		Prerelease: cfg.Channel != "stable",
		Assets:     assets,
		Version:    strings.TrimSpace(info.Version),
		Commit:     strings.TrimSpace(info.Commit),
		BuildTime:  strings.TrimSpace(info.BuildTime),
	}, nil
}

func (u *Updater) fetchReleaseViaProxy(ctx context.Context, cfg Config) (*releaseInfo, error) {
	tag := "latest"
	if cfg.Channel != "stable" {
		tag = "dev"
	}

	url := fmt.Sprintf("%s/api/releases/%s/%s", strings.TrimRight(cfg.ProxyBaseURL, "/"), cfg.Repo, tag)
	u.logger.Printf("firex: checking %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		u.logger.Printf("firex: no release found for channel %s", cfg.Channel)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var release releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if cfg.Channel != "stable" {
		// Only metadata for the UI and the commit comparison; a miss degrades
		// the dev comparison to run numbers rather than failing the check.
		if err := u.loadReleaseVersion(ctx, cfg, &release); err != nil {
			u.logger.Printf("firex: version metadata unavailable for %s: %v", release.TagName, err)
		}
	}
	return &release, nil
}

func (u *Updater) loadReleaseVersion(ctx context.Context, cfg Config, release *releaseInfo) error {
	var versionAsset *assetInfo
	for i := range release.Assets {
		if release.Assets[i].Name == "version.json" {
			versionAsset = &release.Assets[i]
			break
		}
	}
	if versionAsset == nil {
		return fmt.Errorf("version.json asset not found")
	}

	body, status, err := u.httpGet(ctx, u.resolveDownloadURL(cfg, versionAsset.BrowserDownloadURL), 16*1024)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("version metadata returned status %d", status)
	}

	var info releaseVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("decode version metadata: %w", err)
	}
	release.Version = strings.TrimSpace(info.Version)
	release.Commit = strings.TrimSpace(info.Commit)
	release.BuildTime = strings.TrimSpace(info.BuildTime)
	return nil
}

func (u *Updater) isNewer(release releaseInfo, channel string) bool {
	current := version.Version
	// A local `go build` carries no release identity, so treat any published
	// build as newer.
	if current == "dev" {
		return true
	}
	remoteTag := release.TagName
	if channel == "stable" {
		return semverGreater(remoteTag, current)
	}

	// The dev tag is a rolling pointer, so its name says nothing about age.
	// A different commit is the reliable signal: force-pushes and re-runs make
	// run numbers move backwards.
	remoteCommit := normalizeCommit(release.Commit)
	if remoteCommit == "" {
		remoteCommit = normalizeCommit(release.TargetCommitish)
	}
	currentCommit := normalizeCommit(version.Commit)
	if remoteCommit != "" && currentCommit != "" {
		return remoteCommit != currentCommit
	}

	remoteVersion := release.displayVersion()
	if remoteTag == "dev" && remoteVersion == "dev" {
		u.logger.Printf("firex: dev release missing comparable commit current=%s remote=%s, skipping", current, remoteTag)
		return false
	}

	remoteNum, remoteSHA := parseDevTag(remoteVersion)
	localNum, localSHA := parseDevTag(current)
	if remoteSHA != "" && localSHA != "" && remoteSHA == localSHA {
		return false
	}
	if remoteNum > 0 && localNum > 0 {
		return remoteNum > localNum
	}
	// Refusing to guess beats an update loop that reinstalls forever.
	u.logger.Printf("firex: cannot compare versions current=%s remote=%s, skipping", current, remoteTag)
	return false
}

func normalizeCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" || commit == "unknown" {
		return ""
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func semverGreater(a, b string) bool {
	av := parseSemver(strings.TrimPrefix(a, "v"))
	bv := parseSemver(strings.TrimPrefix(b, "v"))
	for i := 0; i < 3; i++ {
		if av[i] > bv[i] {
			return true
		}
		if av[i] < bv[i] {
			return false
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	var result [3]int
	parts := strings.SplitN(s, ".", 3)
	for i, p := range parts {
		if i >= 3 {
			break
		}
		if idx := strings.IndexByte(p, '-'); idx >= 0 {
			p = p[:idx]
		}
		n, _ := strconv.Atoi(p)
		result[i] = n
	}
	return result
}

// parseDevTag splits the dev-{run}-{yyyymmdd}-{sha} version string the release
// workflow builds. The format is a two-sided contract: changing it in CI means
// changing it here.
func parseDevTag(tag string) (runNumber int, sha string) {
	parts := strings.SplitN(tag, "-", 4)
	if len(parts) >= 4 && parts[0] == "dev" {
		n, _ := strconv.Atoi(parts[1])
		return n, parts[3]
	}
	return 0, ""
}

// targetName is the release asset for this platform. It must stay identical to
// the `target` field of the build matrix in the release workflow: a name only
// one side knows about is a permanent 404 for those machines.
func (u *Updater) targetName() string {
	name := "firex-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func (u *Updater) download(ctx context.Context, cfg Config, release *releaseInfo) (string, error) {
	u.mu.Lock()
	u.status.State = "downloading"
	u.status.Progress = progressDownloadStart
	u.status.DownloadProgress = 0
	u.mu.Unlock()

	targetName := u.targetName()
	var binaryAsset, sumsAsset *assetInfo
	for i := range release.Assets {
		a := &release.Assets[i]
		switch a.Name {
		case targetName:
			binaryAsset = a
		case sumsAssetName:
			sumsAsset = a
		}
	}
	if binaryAsset == nil {
		return "", fmt.Errorf("no asset found for %s in release %s", targetName, release.TagName)
	}
	// An optional checksum is no checksum: without the manifest there is
	// nothing to verify against, so the update stops here.
	if sumsAsset == nil {
		return "", fmt.Errorf("release %s is missing %s, refusing unverified update", release.TagName, sumsAssetName)
	}

	updateDir := filepath.Join(u.dataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0o750); err != nil {
		return "", fmt.Errorf("create update dir: %w", err)
	}

	finalName := "firex-" + sanitizePathPart(release.TagName)
	if runtime.GOOS == "windows" {
		finalName += ".exe"
	}
	tmpPath := filepath.Join(updateDir, finalName+".tmp")
	finalPath := filepath.Join(updateDir, finalName)

	dlCtx, cancelDownload := context.WithTimeout(ctx, downloadTimeout)
	defer cancelDownload()

	downloadURL := u.resolveDownloadURL(cfg, binaryAsset.BrowserDownloadURL)
	if err := u.downloadFile(dlCtx, downloadURL, tmpPath, binaryAsset.Size); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download binary: %w", err)
	}

	u.mu.Lock()
	u.status.Progress = progressVerifyStart
	u.mu.Unlock()

	sumsBody, err := u.fetchAsset(dlCtx, cfg, sumsAsset, 64*1024)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("fetch %s: %w", sumsAssetName, err)
	}
	expectedHash, err := sha256ForTarget(sumsBody, targetName)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	actualHash, err := fileSHA256(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("compute sha256: %w", err)
	}
	if !strings.EqualFold(actualHash, expectedHash) {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	u.logger.Printf("firex: SHA256 verified for %s", release.TagName)

	u.mu.Lock()
	u.status.Progress = progressVerifyDone
	u.mu.Unlock()

	// The verified name is only taken once the bytes are known good, so a
	// half-written or mismatched download can never be applied.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	u.logger.Printf("firex: downloaded %s to %s", release.TagName, finalPath)
	return finalPath, nil
}

// sha256ForTarget picks this platform's line out of the release-wide manifest,
// which is plain `sha256sum` output: "<hash>  <bare file name>".
//
// The name match is exact on purpose. A prefix or substring match would let
// firex-linux-arm select the firex-linux-arm64 line, installing a binary for
// the wrong architecture that passes verification and then fails to start.
func sha256ForTarget(sums []byte, target string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// `sha256sum --binary` marks the name with a leading asterisk.
		if strings.TrimPrefix(fields[1], "*") == target {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no sha256 entry for %s in %s", target, sumsAssetName)
}

// resolveDownloadURL rewrites a GitHub browser_download_url onto the mirror
// when the proxy source is selected; the direct source uses it unchanged.
func (u *Updater) resolveDownloadURL(cfg Config, browserURL string) string {
	if cfg.Source != SourceProxy {
		return browserURL
	}
	base := strings.TrimRight(cfg.ProxyBaseURL, "/")
	const ghPrefix = "https://github.com/"
	if !strings.HasPrefix(browserURL, ghPrefix) {
		return browserURL
	}
	path := strings.TrimPrefix(browserURL, ghPrefix)
	const relSegment = "/releases/download/"
	idx := strings.Index(path, relSegment)
	if idx < 0 {
		return browserURL
	}
	ownerRepo := path[:idx]
	tagAndAsset := path[idx+len(relSegment):]
	return base + "/download/" + ownerRepo + "/" + tagAndAsset
}

func (u *Updater) downloadFile(ctx context.Context, url, destPath string, expectedSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	totalSize := resp.ContentLength
	if totalSize <= 0 && expectedSize > 0 {
		totalSize = expectedSize
	}

	var written int64
	var lastProgress float64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				return wErr
			}
			written += int64(n)
			if totalSize > 0 {
				progress := float64(written) / float64(totalSize) * 100
				if progress-lastProgress >= 1 || progress >= 100 {
					u.mu.Lock()
					u.status.DownloadProgress = clampProgress(progress)
					u.status.Progress = overallDownloadProgress(progress)
					u.mu.Unlock()
					lastProgress = progress
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	u.mu.Lock()
	u.status.DownloadProgress = progressComplete
	u.status.Progress = overallDownloadProgress(progressComplete)
	u.mu.Unlock()
	return nil
}

// httpGet fetches url and returns up to limit bytes of the body with the
// status code. Network errors are returned; HTTP error statuses are not, so
// callers can tell "no such release" from "no network".
func (u *Updater) httpGet(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func (u *Updater) fetchAsset(ctx context.Context, cfg Config, asset *assetInfo, limit int64) ([]byte, error) {
	body, status, err := u.httpGet(ctx, u.resolveDownloadURL(cfg, asset.BrowserDownloadURL), limit)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", asset.Name, status)
	}
	return body, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (u *Updater) applyUpdate(newBinaryPath, tag string) error {
	u.mu.Lock()
	u.status.State = "applying"
	u.status.Progress = progressApplying
	u.mu.Unlock()

	if runtime.GOOS == "windows" {
		return u.applyUpdateWindows(newBinaryPath, tag)
	}
	return u.applyUpdateUnix(newBinaryPath, tag)
}

func (u *Updater) applyUpdateUnix(newBinaryPath, tag string) error {
	if u.hooks.BeforeExec != nil {
		if err := u.hooks.BeforeExec(tag); err != nil {
			return fmt.Errorf("prepare restart: %w", err)
		}
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	// Follow symlinks so a /usr/local/bin shim gets the real file replaced.
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	// Every failure below restores the backup, so a failed install leaves the
	// old binary in place rather than nothing at all.
	backupPath := execPath + ".bak"
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := copyFile(newBinaryPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	if err := os.Chmod(execPath, 0o755); err != nil {
		_ = os.Rename(backupPath, execPath)
		_ = os.Remove(newBinaryPath)
		return fmt.Errorf("chmod new binary: %w", err)
	}

	_ = os.Remove(backupPath)
	_ = os.Remove(newBinaryPath)

	u.logger.Printf("firex: restarting into %s", tag)
	u.mu.Lock()
	u.status.Progress = progressComplete
	u.mu.Unlock()
	return replaceProcess(execPath, os.Args, os.Environ())
}

// applyUpdateWindows hands the swap to a detached PowerShell script, because a
// running executable cannot overwrite itself on Windows.
func (u *Updater) applyUpdateWindows(newBinaryPath, tag string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	execPath, err = filepath.Abs(execPath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	updateDir := filepath.Dir(newBinaryPath)
	scriptPath := filepath.Join(updateDir, "apply-"+sanitizePathPart(tag)+".ps1")
	backupPath := execPath + ".bak"
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		fmt.Sprintf("$pidToWait = %d", os.Getpid()),
		"$exe = " + psQuote(execPath),
		"$new = " + psQuote(newBinaryPath),
		"$bak = " + psQuote(backupPath),
		"$argsList = " + psArray(os.Args[1:]),
		"$workDir = " + psQuote(cwd),
		"while (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 250 }",
		"if (Test-Path $bak) { Remove-Item -Force $bak }",
		"if (Test-Path $exe) { Move-Item -Force $exe $bak }",
		"Copy-Item -Force $new $exe",
		"Remove-Item -Force $new",
		"Start-Process -FilePath $exe -ArgumentList $argsList -WorkingDirectory $workDir",
		"Remove-Item -Force $PSCommandPath",
		"",
	}, "\r\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return fmt.Errorf("write apply script: %w", err)
	}

	if u.hooks.BeforeExec != nil {
		if err := u.hooks.BeforeExec(tag); err != nil {
			return fmt.Errorf("prepare restart: %w", err)
		}
	}

	proc, err := os.StartProcess("powershell.exe", []string{
		"powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
	})
	if err != nil {
		return fmt.Errorf("start apply script: %w", err)
	}
	_ = proc.Release()

	u.logger.Printf("firex: restarting into %s", tag)
	u.mu.Lock()
	u.status.Progress = progressComplete
	u.mu.Unlock()
	os.Exit(0)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Normalize fills in defaults and canonical casing. Callers apply it once at
// load time so the whole process agrees on the settings the updater will use;
// the updater applies it again on every read rather than trusting them.
func Normalize(cfg Config) Config { return normalizeConfig(cfg) }

// normalizeConfig fills in defaults rather than trusting the caller, so a
// half-populated config still points somewhere valid.
func normalizeConfig(cfg Config) Config {
	cfg.Channel = strings.ToLower(strings.TrimSpace(cfg.Channel))
	if cfg.Channel != "dev" {
		cfg.Channel = "stable"
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 3600
	}
	cfg.Source = strings.ToLower(strings.TrimSpace(cfg.Source))
	if cfg.Source != SourceProxy {
		cfg.Source = SourceGitHub
	}
	cfg.ProxyBaseURL = strings.TrimRight(strings.TrimSpace(cfg.ProxyBaseURL), "/")
	if cfg.ProxyBaseURL == "" {
		cfg.ProxyBaseURL = DefaultProxyBaseURL
	}
	cfg.Repo = strings.Trim(strings.TrimSpace(cfg.Repo), "/")
	if cfg.Repo == "" {
		cfg.Repo = DefaultRepo
	}
	return cfg
}

// sanitizePathPart keeps a release tag from escaping the updates directory.
func sanitizePathPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "update"
	}
	return b.String()
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func psArray(values []string) string {
	if len(values) == 0 {
		return "@()"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, psQuote(value))
	}
	return "@(" + strings.Join(quoted, ", ") + ")"
}
