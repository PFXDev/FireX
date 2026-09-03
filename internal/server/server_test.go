package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/paneltest"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
	"github.com/PFXDev/FireX/internal/updater"
)

type harness struct {
	t      *testing.T
	db     *store.DB
	server *httptest.Server
	client *http.Client
	fake   *paneltest.Panel
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := paneltest.New("tok",
		paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "hk", Enable: true},
		paneltest.Inbound{ID: 2, Port: 8443, Protocol: "vless", Remark: "jp", Enable: true},
	)
	t.Cleanup(fake.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// main seeds the stock matrix on every start; without it there would be no
	// policies and every subscription would render an empty rule list.
	if err := routing.Seed(db); err != nil {
		t.Fatalf("routing.Seed() error = %v", err)
	}

	if _, _, err := EnsureAdmin(db, "admin", "password123"); err != nil {
		t.Fatalf("EnsureAdmin() error = %v", err)
	}

	cfg := &config.Config{}
	mgr := provision.NewManager(db)
	srv := New(cfg, db, mgr, subscription.NewService(db, mgr), updater.New(
		func() updater.Config { return cfg.Update.Updater() },
		func() string { return cfg.DataDir },
		log.New(io.Discard, "", 0),
		updater.RestartHooks{},
	))
	ts := httptest.NewServer(srv.engine)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	return &harness{t: t, db: db, server: ts, client: &http.Client{Jar: jar}, fake: fake}
}

func (h *harness) do(method, path string, body any) (*http.Response, []byte) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
		reader = strings.NewReader(string(buf))
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// mustDo fails the test unless the call returned 200.
func (h *harness) mustDo(method, path string, body any) []byte {
	h.t.Helper()
	resp, raw := h.do(method, path, body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("%s %s = %d: %s", method, path, resp.StatusCode, raw)
	}
	return raw
}

func (h *harness) login() {
	h.t.Helper()
	h.mustDo(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin", "password": "password123",
	})
}

// seed walks the whole admin flow: add a panel, label and enable its inbounds,
// put each in its own node group, whitelist both in a profile, bind a plan to
// that profile, and put one user on it.
func (h *harness) seed() *model.User {
	h.t.Helper()
	h.login()
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p1", "baseUrl": h.fake.URL(), "apiToken": "tok",
	})

	var inbounds []struct {
		ID   uint `json:"id"`
		Port int  `json:"port"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/inbounds", nil), &inbounds); err != nil {
		h.t.Fatalf("decode inbounds: %v", err)
	}
	if len(inbounds) != 2 {
		h.t.Fatalf("inbounds = %d, want 2", len(inbounds))
	}

	labels := map[int]string{443: "🇭🇰 香港", 8443: "🇯🇵 日本"}
	groupIDs := make([]uint, 0, len(inbounds))
	for _, n := range inbounds {
		h.mustDo(http.MethodPut, "/api/inbounds/"+itoa(n.ID), map[string]any{
			"name": labels[n.Port], "enabled": true,
		})
		var group struct {
			ID uint `json:"id"`
		}
		raw := h.mustDo(http.MethodPost, "/api/node-groups", map[string]any{
			"name": labels[n.Port], "type": "url-test", "inboundIds": []uint{n.ID},
			"tags": []map[string]string{{"key": "地区", "value": labels[n.Port]}},
		})
		json.Unmarshal(raw, &group)
		groupIDs = append(groupIDs, group.ID)
	}

	var profile struct {
		ID uint `json:"id"`
	}
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/profiles", map[string]any{
		"name": "标准分流", "groupIds": groupIDs,
	}), &profile)

	h.mustDo(http.MethodPost, "/api/plans", map[string]any{
		"name": "标准", "profileId": profile.ID,
		"trafficBytes": 10 << 30, "durationDays": 30, "deviceLimit": 3,
	})

	var plans []struct {
		ID uint `json:"id"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/plans", nil), &plans)
	h.mustDo(http.MethodPost, "/api/users", map[string]any{"username": "carol", "planId": plans[0].ID})

	var u model.User
	if err := h.db.First(&u, "username = ?", "carol").Error; err != nil {
		h.t.Fatalf("user not created: %v", err)
	}
	return &u
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestAdminAPIRequiresSession(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(http.MethodGet, "/api/users", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/users without a session = %d, want 401", resp.StatusCode)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin", "password": "nope",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with a wrong password = %d, want 401", resp.StatusCode)
	}
}

func TestPanelTokenNeverLeavesTheServer(t *testing.T) {
	h := newHarness(t)
	h.seed()
	raw := h.mustDo(http.MethodGet, "/api/panels", nil)
	if strings.Contains(string(raw), "tok") && strings.Contains(string(raw), "apiToken\":\"tok") {
		t.Errorf("panel list leaked the API token: %s", raw)
	}
}

func TestFullFlowServesClashSubscription(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/sub/"+u.SubToken, nil)
	req.Header.Set("User-Agent", "clash-verge/2.0")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("fetch subscription: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sub = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Subscription-Userinfo"); !strings.Contains(got, "total=") {
		t.Errorf("Subscription-Userinfo = %q", got)
	}

	var cfg struct {
		Proxies []struct {
			Name string `yaml:"name"`
			UUID string `yaml:"uuid"`
		} `yaml:"proxies"`
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("subscription is not valid YAML: %v\n%s", err, body)
	}
	if len(cfg.Proxies) != 2 {
		t.Fatalf("proxies = %d, want 2:\n%s", len(cfg.Proxies), body)
	}
	if cfg.Proxies[0].UUID != u.UUID {
		t.Errorf("proxy uuid = %q, want the user's %q", cfg.Proxies[0].UUID, u.UUID)
	}
	var groupNames []string
	for _, g := range cfg.Groups {
		groupNames = append(groupNames, g.Name)
		if len(g.Proxies) == 0 {
			t.Errorf("group %q is empty; mihomo refuses to load that", g.Name)
		}
	}
	for _, want := range []string{"🇭🇰 香港", "🇯🇵 日本", "🚀 节点选择"} {
		if !containsStr(groupNames, want) {
			t.Errorf("group %q missing from %v", want, groupNames)
		}
	}
	if len(cfg.Rules) == 0 {
		t.Error("rules are empty")
	}
	if last := cfg.Rules[len(cfg.Rules)-1]; !strings.HasPrefix(last, "MATCH,") {
		t.Errorf("last rule = %q, want the MATCH line", last)
	}
}

func TestKnownLegacyClientReceivesBase64(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/sub/"+u.SubToken, nil)
	req.Header.Set("User-Agent", "v2rayN/7.0")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("fetch subscription: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	decoded, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		t.Fatalf("body is not base64: %v\n%s", err, body)
	}
	links := strings.Split(strings.TrimSpace(string(decoded)), "\n")
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2:\n%s", len(links), decoded)
	}
	for _, link := range links {
		if !strings.HasPrefix(link, "vless://"+u.UUID+"@") {
			t.Errorf("link %q does not carry the user's uuid", link)
		}
	}
}

func TestSubscriptionDefaultsToMihomo(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	resp, body := h.do(http.MethodGet, "/sub/"+u.SubToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sub = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "proxy-groups:") {
		t.Errorf("default subscription is not a mihomo profile:\n%s", body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/yaml") {
		t.Errorf("Content-Type = %q, want text/yaml", got)
	}
}

func TestSubscriptionTargetQueryOverridesUserAgent(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/sub/"+u.SubToken+"?target=clash", nil)
	req.Header.Set("User-Agent", "v2rayN/7.0")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("fetch subscription: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "proxy-groups") {
		t.Errorf("?target=clash did not win over the user agent:\n%s", body)
	}
}

func TestMihomoTargetAlias(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	resp, body := h.do(http.MethodGet, "/sub/"+u.SubToken+"?target=mihomo", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sub?target=mihomo = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "proxy-groups:") {
		t.Errorf("?target=mihomo did not return a mihomo profile:\n%s", body)
	}
}

func TestSingBoxTargetIsDisabled(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	resp, body := h.do(http.MethodGet, "/sub/"+u.SubToken+"?target=sing-box", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /sub?target=sing-box = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestUnknownSubTokenIs404(t *testing.T) {
	h := newHarness(t)
	h.seed()
	resp, _ := h.do(http.MethodGet, "/sub/definitely-not-a-token", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown token = %d, want 404", resp.StatusCode)
	}
}

func TestDeletingUserRemovesRemoteClient(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	if h.fake.Client("carol@firex") == nil {
		t.Fatal("client was never created on the panel")
	}
	h.mustDo(http.MethodDelete, "/api/users/"+itoa(u.ID), nil)
	if c := h.fake.Client("carol@firex"); c != nil {
		t.Errorf("client %+v left behind on the panel after the user was deleted", c)
	}
}

func TestUsernameIsImmutable(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	resp, raw := h.do(http.MethodPut, "/api/users/"+itoa(u.ID), map[string]any{"username": "dave", "planId": u.PlanID})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("rename = %d (%s), want 400 — renaming would orphan the panel clients", resp.StatusCode, raw)
	}
}

func TestRejectsBadUsername(t *testing.T) {
	h := newHarness(t)
	h.seed()
	for _, name := range []string{"a", "has space", "sla/sh", strings.Repeat("x", 41)} {
		resp, _ := h.do(http.MethodPost, "/api/users", map[string]any{"username": name})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("username %q accepted with %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestBrokenClashTemplateIsRejected(t *testing.T) {
	h := newHarness(t)
	h.login()
	resp, _ := h.do(http.MethodPut, "/api/settings/clashTemplate", map[string]string{
		"template": "dns:\n  enable: [\n",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad template = %d, want 400 before it reaches a client", resp.StatusCode)
	}
	// The stored template must still be the working one.
	if got := h.db.GetSetting(subscription.SettingKeyClashTemplate, ""); got != "" {
		t.Errorf("stored template = %q, want it untouched", got)
	}
}

func TestLiveInboundCannotBeDeleted(t *testing.T) {
	h := newHarness(t)
	h.seed()
	var inbound model.Inbound
	h.db.First(&inbound)
	resp, _ := h.do(http.MethodDelete, "/api/inbounds/"+itoa(inbound.ID), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("delete live inbound = %d, want 409", resp.StatusCode)
	}
}

// A routing save that would produce a config mihomo cannot load must not land in
// the database at all — the transaction rolls back on validation failure.
func TestInvalidRoutingSaveIsRolledBack(t *testing.T) {
	h := newHarness(t)
	h.seed()

	var before int64
	h.db.Model(&model.Policy{}).Count(&before)
	resp, _ := h.do(http.MethodPut, "/api/routing", map[string]any{
		"policies": []map[string]any{
			{"name": "坏策略", "enabled": true, "isFinal": true, "rules": []map[string]any{
				{"type": "NOT-A-MATCHER", "value": "x"},
			}},
		},
		"egresses": []map[string]any{
			{"policyIndex": 0, "profileId": 0, "type": "select",
				"members": []map[string]string{{"kind": "builtin", "ref": "DIRECT"}}},
		},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("save with an unknown matcher = %d, want 400", resp.StatusCode)
	}
	var after int64
	h.db.Model(&model.Policy{}).Count(&after)
	if after != before {
		t.Errorf("policies = %d after a rejected save, want the original %d", after, before)
	}
}

// Editing rules changes what a client renders but never which inbounds a user
// holds, so it must not write to a panel.
func TestRoutingSaveDoesNotTouchPanels(t *testing.T) {
	h := newHarness(t)
	h.seed()
	raw := h.mustDo(http.MethodGet, "/api/routing", nil)
	var doc struct {
		Policies []map[string]any `json:"policies"`
		Egresses []map[string]any `json:"egresses"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode routing: %v", err)
	}

	h.fake.ResetCalls()
	h.mustDo(http.MethodPut, "/api/routing", doc)
	for _, call := range h.fake.Calls() {
		if strings.HasPrefix(call, "POST ") {
			t.Errorf("routing save wrote to a panel: %s (all: %v)", call, h.fake.Calls())
		}
	}
}

func TestArrayFieldsNeverSerializeAsNull(t *testing.T) {
	// A nil Go slice marshals as null, and the admin UI reads these fields as
	// arrays — `warnings.length` on null takes the whole page down.
	h := newHarness(t)
	u := h.seed()
	// A node group with no inbounds and a user whose subscription produced no
	// warnings are exactly the cases that yield nil slices.
	h.mustDo(http.MethodPost, "/api/node-groups", map[string]any{"name": "空分组", "inboundIds": []uint{}})
	h.mustDo(http.MethodPost, "/api/profiles", map[string]any{"name": "空方案", "groupIds": []uint{}})

	cases := []struct {
		path string
		keys []string
	}{
		{"/api/node-groups", []string{"inboundIds", "tags"}},
		{"/api/profiles", []string{"groupIds"}},
		{"/api/routing", []string{"policies", "egresses", "rules", "members"}},
		{"/api/users", []string{"syncErrors"}},
		{"/api/users/" + itoa(u.ID) + "/subscription", []string{"warnings", "entries"}},
	}
	for _, tc := range cases {
		raw := h.mustDo(http.MethodGet, tc.path, nil)
		assertNoNullFields(t, tc.path, raw, tc.keys...)
	}
}

func assertNoNullFields(t *testing.T, path string, raw []byte, keys ...string) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: invalid JSON: %v", path, err)
	}
	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		wanted[k] = true
	}
	var walk func(any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			for key, value := range node {
				if wanted[key] && value == nil {
					t.Errorf("%s: %q serialized as null, want []", path, key)
				}
				walk(value)
			}
		case []any:
			for _, item := range node {
				walk(item)
			}
		}
	}
	walk(doc)
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
