package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Read returns the editable file form without writing or resolving dbPath.
// The same completion rules as startup support files from older releases.
func Read(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := Template()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.complete()
	return cfg, nil
}

// RequiresRestart compares the file's effective values to the startup snapshot.
// A pending password reset needs a restart even if other settings are unchanged.
func (c Config) RequiresRestart(running *Config) bool {
	if c.AdminPassword != "" {
		return true
	}
	c.resolve()
	active := *running
	active.AdminPassword = ""
	return c != active
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`)

// Validate checks an explicit edit rather than silently replacing invalid
// input with defaults, as startup completion does for old hand-written files.
func (c *Config) Validate() map[string]string {
	fields := make(map[string]string)
	c.Listen = strings.TrimSpace(c.Listen)
	c.DataDir = strings.TrimSpace(c.DataDir)
	c.DBPath = strings.TrimSpace(c.DBPath)
	c.SubBaseURL = strings.TrimRight(strings.TrimSpace(c.SubBaseURL), "/")
	c.AdminUser = strings.TrimSpace(c.AdminUser)
	c.Update.Channel = strings.ToLower(strings.TrimSpace(c.Update.Channel))
	c.Update.Source = strings.ToLower(strings.TrimSpace(c.Update.Source))
	c.Update.ProxyBaseURL = strings.TrimRight(strings.TrimSpace(c.Update.ProxyBaseURL), "/")
	c.Update.Repo = strings.Trim(strings.TrimSpace(c.Update.Repo), "/")

	_, port, err := net.SplitHostPort(c.Listen)
	p, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || p < 1 || p > 65535 || strings.ContainsAny(c.Listen, " /\t\r\n") {
		fields["listen"] = "请输入有效的监听地址，例如 :8080 或 127.0.0.1:8080（端口 1–65535）。"
	}
	if c.DataDir == "" || strings.ContainsRune(c.DataDir, 0) {
		fields["dataDir"] = "数据目录不能为空或包含空字符。"
	}
	if strings.ContainsRune(c.DBPath, 0) {
		fields["dbPath"] = "数据库路径不能包含空字符。"
	}
	if c.SubBaseURL != "" && !validHTTPURL(c.SubBaseURL) {
		fields["subBaseUrl"] = "请输入完整的 HTTP(S) 地址，不含账号、查询参数或片段。"
	}
	if c.AdminUser == "" {
		fields["adminUser"] = "管理员用户名不能为空。"
	}
	if c.AdminPassword != "" && (len(c.AdminPassword) < 8 || len(c.AdminPassword) > 72) {
		fields["adminPassword"] = "重置密码需要 8–72 字节。"
	}
	for _, d := range []struct {
		key   string
		value Duration
	}{
		{"syncInterval", c.SyncInterval},
		{"trafficInterval", c.TrafficInterval},
		{"discoverInterval", c.DiscoverInterval},
	} {
		if d.value <= 0 {
			fields[d.key] = "时间间隔必须大于 0，例如 30s、2m 或 1h。"
		}
	}
	if c.Update.CheckInterval < Duration(time.Minute) {
		fields["update.checkInterval"] = "更新检查间隔至少为 1m。"
	}
	if c.Update.Channel != "stable" && c.Update.Channel != "dev" {
		fields["update.channel"] = "更新通道只能是 stable 或 dev。"
	}
	if c.Update.Source != "github" && c.Update.Source != "proxy" {
		fields["update.source"] = "更新来源只能是 github 或 proxy。"
	}
	if (c.Update.Source == "proxy" || c.Update.ProxyBaseURL != "") && !validHTTPURL(c.Update.ProxyBaseURL) {
		fields["update.proxyBaseUrl"] = "请输入完整的 HTTP(S) 镜像地址，不含账号、查询参数或片段。"
	}
	if !repoPattern.MatchString(c.Update.Repo) || strings.HasSuffix(c.Update.Repo, "/.") || strings.HasSuffix(c.Update.Repo, "/..") {
		fields["update.repo"] = "请输入 owner/name 格式的仓库名称。"
	}
	if len(fields) == 0 {
		c.complete()
	}
	return fields
}

func validHTTPURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" &&
		u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, " \t\r\n")
}
