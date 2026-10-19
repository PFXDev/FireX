package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/model"
)

// bearer calls the API with an API token and no cookie jar, the way an agent
// would.
func (h *harness) bearer(method, path, token string, body any) (*http.Response, []byte) {
	h.t.Helper()
	var reader *strings.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = strings.NewReader(string(buf))
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func (h *harness) mintToken(readOnly bool) (string, model.APIToken) {
	h.t.Helper()
	var created struct {
		Token    string         `json:"token"`
		APIToken model.APIToken `json:"apiToken"`
	}
	raw := h.mustDo(http.MethodPost, "/api/tokens", map[string]any{"name": "agent", "readOnly": readOnly})
	if err := json.Unmarshal(raw, &created); err != nil {
		h.t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, apiTokenPrefix) || !strings.HasPrefix(created.Token, created.APIToken.Prefix) {
		h.t.Fatalf("token %q does not start with prefix %q", created.Token, created.APIToken.Prefix)
	}
	return created.Token, created.APIToken
}

func TestAPITokenAuthenticatesWithoutSession(t *testing.T) {
	h := newHarness(t)
	h.login()
	token, row := h.mintToken(false)

	if resp, raw := h.bearer(http.MethodGet, "/api/overview", token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/overview with token = %d: %s", resp.StatusCode, raw)
	}
	var stored model.APIToken
	h.db.First(&stored, row.ID)
	if stored.LastUsedAt == 0 {
		t.Fatal("last used time was not recorded")
	}
	if stored.Hash == "" || strings.Contains(string(h.mustDo(http.MethodGet, "/api/tokens", nil)), stored.Hash) {
		t.Fatal("token list must not expose the hash")
	}
	if resp, _ := h.bearer(http.MethodGet, "/api/overview", token+"x", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
	}
}

func TestAPITokenCannotManageCredentials(t *testing.T) {
	h := newHarness(t)
	h.login()
	token, _ := h.mintToken(false)

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/tokens"},
		{http.MethodPost, "/api/tokens"},
		{http.MethodDelete, "/api/tokens/1"},
		{http.MethodPost, "/api/auth/password"},
	} {
		resp, raw := h.bearer(call.method, call.path, token, map[string]any{"name": "x", "current": "password123", "new": "password456"})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s with token = %d, want 403: %s", call.method, call.path, resp.StatusCode, raw)
		}
	}
}

func TestReadOnlyTokenMayReadAndPreviewOnly(t *testing.T) {
	h := newHarness(t)
	h.login()
	token, _ := h.mintToken(true)

	resp, raw := h.bearer(http.MethodGet, "/api/snapshot", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/snapshot = %d: %s", resp.StatusCode, raw)
	}
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	if resp, raw := h.bearer(http.MethodPost, "/api/snapshot/preview", token, doc); resp.StatusCode != http.StatusOK {
		t.Fatalf("preview with read-only token = %d: %s", resp.StatusCode, raw)
	}
	if resp, _ := h.bearer(http.MethodPut, "/api/snapshot", token, doc); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("apply with read-only token = %d, want 403", resp.StatusCode)
	}
	if resp, _ := h.bearer(http.MethodPost, "/api/sync", token, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("sync with read-only token = %d, want 403", resp.StatusCode)
	}
}

func TestRevokedAndExpiredTokensAreRejected(t *testing.T) {
	h := newHarness(t)
	h.login()
	revoked, row := h.mintToken(false)
	h.mustDo(http.MethodDelete, "/api/tokens/"+itoa(row.ID), nil)
	if resp, _ := h.bearer(http.MethodGet, "/api/overview", revoked, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token = %d, want 401", resp.StatusCode)
	}

	expired, row := h.mintToken(false)
	h.db.Model(&model.APIToken{}).Where("id = ?", row.ID).Update("expires_at", time.Now().Add(-time.Minute).UnixMilli())
	if resp, _ := h.bearer(http.MethodGet, "/api/overview", expired, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired token = %d, want 401", resp.StatusCode)
	}
}

// A bad bearer header must not fall back to the browser session riding along.
func TestBearerHeaderNeverFallsBackToCookie(t *testing.T) {
	h := newHarness(t)
	h.login()
	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/overview", nil)
	req.Header.Set("Authorization", "Bearer fxt_nope")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad bearer with valid cookie = %d, want 401", resp.StatusCode)
	}
}
