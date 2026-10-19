package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
)

// API tokens let a script or a local agent drive the admin API. A token acts
// as the admin that created it, except that it can never manage credentials:
// minting, listing and revoking tokens and changing the password need a
// signed-in session, so a leaked token cannot entrench itself.

const (
	apiTokenPrefix = "fxt_"
	ctxViaTokenKey = "firex_via_token"
	// lastUsedEvery throttles the last-used stamp so a busy agent does not
	// turn every read into a database write.
	lastUsedEvery = time.Minute
)

func hashAPIToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// readOnlySafe lists the requests a read-only token may make. Previewing a
// snapshot is a POST but rolls everything back.
func readOnlySafe(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
		return true
	}
	return c.Request.Method == http.MethodPost && c.FullPath() == "/api/snapshot/preview"
}

func (s *Server) requireAPIToken(c *gin.Context, header string) {
	scheme, token, ok := strings.Cut(header, " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || !strings.HasPrefix(token, apiTokenPrefix) {
		failMsg(c, http.StatusUnauthorized, "invalid API token")
		return
	}
	var row model.APIToken
	if err := s.db.First(&row, "hash = ?", hashAPIToken(token)).Error; err != nil {
		failMsg(c, http.StatusUnauthorized, "invalid API token")
		return
	}
	now := time.Now().UnixMilli()
	if row.ExpiresAt > 0 && row.ExpiresAt <= now {
		failMsg(c, http.StatusUnauthorized, "API token expired")
		return
	}
	var admin model.Admin
	if err := s.db.First(&admin, row.AdminID).Error; err != nil {
		failMsg(c, http.StatusUnauthorized, "invalid API token")
		return
	}
	if row.ReadOnly && !readOnlySafe(c) {
		failMsg(c, http.StatusForbidden, "this API token is read-only")
		return
	}
	if now-row.LastUsedAt >= lastUsedEvery.Milliseconds() {
		s.db.Model(&model.APIToken{}).Where("id = ?", row.ID).Update("last_used_at", now)
	}
	c.Set(ctxAdminKey, &admin)
	c.Set(ctxViaTokenKey, true)
	c.Next()
}

// requireSession rejects API tokens on the routes that manage credentials.
func requireSession(c *gin.Context) {
	if c.GetBool(ctxViaTokenKey) {
		failMsg(c, http.StatusForbidden, "API tokens cannot manage credentials; sign in to the admin UI")
		return
	}
	c.Next()
}

func (s *Server) listAPITokens(c *gin.Context) {
	var rows []model.APIToken
	if err := s.db.Where("admin_id = ?", currentAdmin(c).ID).Order("id DESC").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, jsonList(rows))
}

type apiTokenRequest struct {
	Name     string `json:"name"`
	ReadOnly bool   `json:"readOnly"`
	// ExpiresInDays is 0 for a token that never expires.
	ExpiresInDays int `json:"expiresInDays"`
}

func (s *Server) createAPIToken(c *gin.Context) {
	var req apiTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		failMsg(c, http.StatusBadRequest, "令牌名称不能为空且不能超过 100 字节")
		return
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		failMsg(c, http.StatusBadRequest, "有效期必须在 0 到 3650 天之间")
		return
	}
	token := apiTokenPrefix + randomToken(32)
	now := time.Now()
	row := model.APIToken{
		AdminID:   currentAdmin(c).ID,
		Name:      name,
		Hash:      hashAPIToken(token),
		Prefix:    token[:len(apiTokenPrefix)+8],
		ReadOnly:  req.ReadOnly,
		CreatedAt: now.UnixMilli(),
	}
	if req.ExpiresInDays > 0 {
		row.ExpiresAt = now.AddDate(0, 0, req.ExpiresInDays).UnixMilli()
	}
	if err := s.db.Create(&row).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"token": token, "apiToken": row})
}

func (s *Server) deleteAPIToken(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	result := s.db.Where("id = ? AND admin_id = ?", id, currentAdmin(c).ID).Delete(&model.APIToken{})
	if result.Error != nil {
		fail(c, http.StatusInternalServerError, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		failMsg(c, http.StatusNotFound, "令牌不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
