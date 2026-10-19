package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/PFXDev/FireX/internal/model"
)

// makeOperator seeds an operator directly so the role can be exercised without
// going through the super-admin endpoint under test.
func makeOperator(t *testing.T, h *harness, username string) *model.Admin {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := model.Admin{
		Username: username, PasswordHash: string(hash), Role: model.AdminRoleOperator,
	}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatalf("create operator: %v", err)
	}
	return &admin
}

func TestOperatorManagesUsersButNotInfrastructure(t *testing.T) {
	h := newHarness(t)
	user := h.seed()
	staff := makeOperator(t, h, "staff")
	h.loginAs("staff", "password123")

	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/overview"},
		{http.MethodGet, "/api/users"},
		{http.MethodGet, "/api/plans"},
		{http.MethodGet, "/api/profiles"},
		{http.MethodGet, "/api/users/" + itoa(user.ID) + "/subscription"},
		{http.MethodPost, "/api/users/" + itoa(user.ID) + "/resync"},
		{http.MethodPost, "/api/users"},
	}
	for _, req := range allowed {
		resp, raw := h.do(req.method, req.path, map[string]any{"username": "newbie", "planId": 0})
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s = %d, want 200: %s", req.method, req.path, resp.StatusCode, raw)
		}
	}

	denied := []struct{ method, path string }{
		{http.MethodGet, "/api/panels"},
		{http.MethodPost, "/api/panels"},
		{http.MethodPost, "/api/sync"},
		{http.MethodGet, "/api/routing"},
		{http.MethodPut, "/api/routing"},
		{http.MethodGet, "/api/node-groups"},
		{http.MethodPost, "/api/node-groups"},
		{http.MethodGet, "/api/audit-logs"},
		{http.MethodGet, "/api/settings/clashTemplate"},
		{http.MethodGet, "/api/settings/server"},
		{http.MethodGet, "/api/version"},
		{http.MethodGet, "/api/agent-sessions"},
		{http.MethodPost, "/api/plans"},
		{http.MethodGet, "/api/admins"},
	}
	for _, req := range denied {
		resp, raw := h.do(req.method, req.path, map[string]any{"name": "x"})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403: %s", req.method, req.path, resp.StatusCode, raw)
		}
	}

	// An operator's user changes stay attributable in the log, which only the
	// super admin can read.
	h.login()
	var logs struct {
		Items []struct {
			Actor, Action, Target string
		} `json:"items"`
	}
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/audit-logs", nil), &logs); err != nil {
		t.Fatalf("decode audit log: %v", err)
	}
	found := false
	for _, item := range logs.Items {
		if item.Actor == staff.Username && item.Action == "新增用户" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit log has no 新增用户 entry by %q", staff.Username)
	}
}

func TestSuperAdminManagesAdmins(t *testing.T) {
	h := newHarness(t)
	h.login()

	if resp, raw := h.do(http.MethodPost, "/api/admins", map[string]string{
		"username": "staff", "password": "short", "role": "operator",
	}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password = %d, want 400: %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do(http.MethodPost, "/api/admins", map[string]string{
		"username": "staff", "password": "password123", "role": "root",
	}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown role = %d, want 400: %s", resp.StatusCode, raw)
	}

	var created struct {
		ID       uint   `json:"id"`
		Role     string `json:"role"`
		Password string `json:"password"`
		Hash     string `json:"passwordHash"`
	}
	raw := h.mustDo(http.MethodPost, "/api/admins", map[string]string{
		"username": "staff", "password": "password123", "role": "",
	})
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode admin: %v", err)
	}
	if created.Role != model.AdminRoleOperator {
		t.Fatalf("default role = %q, want operator", created.Role)
	}
	if created.Hash != "" || created.Password != "" {
		t.Fatalf("password material leaked in the response: %s", raw)
	}

	// The new admin can sign in, and the role follows the account.
	h.loginAs("staff", "password123")
	var me struct{ Username, Role string }
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/auth/me", nil), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Username != "staff" || me.Role != model.AdminRoleOperator {
		t.Fatalf("me = %+v, want staff/operator", me)
	}

	// Promoting works; the changed role takes effect on the next request.
	h.login()
	h.mustDo(http.MethodPut, "/api/admins/"+itoa(created.ID), map[string]string{
		"username": "staff", "role": model.AdminRoleSuperAdmin,
	})
	var promoted model.Admin
	if err := h.db.First(&promoted, created.ID).Error; err != nil {
		t.Fatalf("load promoted admin: %v", err)
	}
	if promoted.Role != model.AdminRoleSuperAdmin {
		t.Fatalf("role = %q after promote, want super_admin", promoted.Role)
	}

	// Deleting removes the account outright.
	h.mustDo(http.MethodDelete, "/api/admins/"+itoa(created.ID), nil)
	if err := h.db.First(&model.Admin{}, created.ID).Error; err == nil {
		t.Fatalf("admin %d still exists after delete", created.ID)
	}
}

func TestCannotRemoveTheLastSuperAdmin(t *testing.T) {
	h := newHarness(t)
	h.login()

	var self model.Admin
	if err := h.db.First(&self, "username = ?", "admin").Error; err != nil {
		t.Fatalf("load bootstrap admin: %v", err)
	}

	if resp, raw := h.do(http.MethodPut, "/api/admins/"+itoa(self.ID), map[string]string{
		"username": self.Username, "role": model.AdminRoleOperator,
	}); resp.StatusCode != http.StatusConflict {
		t.Errorf("demote last super admin = %d, want 409: %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do(http.MethodDelete, "/api/admins/"+itoa(self.ID), nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete own account = %d, want 409: %s", resp.StatusCode, raw)
	}

	// Peers can be removed; only the last super admin is protected.
	staff := makeOperator(t, h, "staff")
	h.mustDo(http.MethodDelete, "/api/admins/"+itoa(staff.ID), nil)
}

func TestAdminPasswordResetSignsOutTheTarget(t *testing.T) {
	h := newHarness(t)
	h.login()
	staff := makeOperator(t, h, "staff")

	// Sign in as the operator so there is a session to invalidate.
	h.loginAs("staff", "password123")
	operatorJar := h.client.Jar

	h.login()
	if resp, raw := h.do(http.MethodPost, "/api/admins/"+itoa(staff.ID)+"/resetPassword", map[string]string{
		"password": "short",
	}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short reset password = %d, want 400: %s", resp.StatusCode, raw)
	}
	h.mustDo(http.MethodPost, "/api/admins/"+itoa(staff.ID)+"/resetPassword", map[string]string{
		"password": "brandnew123",
	})

	// The operator's old cookie no longer works.
	h.client.Jar = operatorJar
	if resp, _ := h.do(http.MethodGet, "/api/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/auth/me with a reset session = %d, want 401", resp.StatusCode)
	}
	// Nor does the old password; the new one does.
	h.client.Jar, _ = cookiejar.New(nil)
	if resp, _ := h.do(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "staff", "password": "password123",
	}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old password after reset = %d, want 401", resp.StatusCode)
	}
	h.mustDo(http.MethodPost, "/api/auth/login", map[string]string{
		"username": "staff", "password": "brandnew123",
	})
}

// A reset of one's own password through the admin endpoint would bypass the
// current-password check, so it is refused with a pointer to the settings page.
func TestResetOwnPasswordGoesThroughTheAccountPage(t *testing.T) {
	h := newHarness(t)
	h.login()
	var self model.Admin
	if err := h.db.First(&self, "username = ?", "admin").Error; err != nil {
		t.Fatalf("load bootstrap admin: %v", err)
	}
	resp, raw := h.do(http.MethodPost, "/api/admins/"+itoa(self.ID)+"/resetPassword", map[string]string{
		"password": "brandnew123",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reset own password = %d, want 400: %s", resp.StatusCode, raw)
	}
}
