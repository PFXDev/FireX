package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

const (
	sessionCookie = "firex_session"
	sessionTTL    = 7 * 24 * time.Hour
	ctxAdminKey   = "firex_admin"
)

// EnsureAdmin creates the bootstrap admin on first run. A blank password means
// one is generated and returned so it can be printed once to the operator.
func EnsureAdmin(db *store.DB, username, password string) (created bool, generated string, err error) {
	var count int64
	if err := db.Model(&model.Admin{}).Count(&count).Error; err != nil {
		return false, "", err
	}
	if count > 0 {
		return false, "", nil
	}
	if username == "" {
		username = "admin"
	}
	if password == "" {
		password = randomToken(12)
		generated = password
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, "", err
	}
	now := time.Now().UnixMilli()
	admin := model.Admin{Username: username, PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&admin).Error; err != nil {
		return false, "", err
	}
	return true, generated, nil
}

func randomToken(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is unrecoverable for anything token-shaped.
		panic("firex: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func (s *Server) requireAdmin(c *gin.Context) {
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		failMsg(c, http.StatusUnauthorized, "not signed in")
		return
	}
	var sess model.Session
	if err := s.db.First(&sess, "token = ?", token).Error; err != nil {
		failMsg(c, http.StatusUnauthorized, "not signed in")
		return
	}
	if sess.ExpiresAt <= time.Now().UnixMilli() {
		s.db.Delete(&model.Session{}, "token = ?", token)
		failMsg(c, http.StatusUnauthorized, "session expired")
		return
	}
	var admin model.Admin
	if err := s.db.First(&admin, sess.AdminID).Error; err != nil {
		failMsg(c, http.StatusUnauthorized, "not signed in")
		return
	}
	c.Set(ctxAdminKey, &admin)
	c.Next()
}

func currentAdmin(c *gin.Context) *model.Admin {
	value, ok := c.Get(ctxAdminKey)
	if !ok {
		return nil
	}
	admin, _ := value.(*model.Admin)
	return admin
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var admin model.Admin
	err := s.db.First(&admin, "username = ?", strings.TrimSpace(req.Username)).Error
	if err != nil {
		// Run a comparison anyway so a wrong username and a wrong password take
		// the same time to reject.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalidin"), []byte(req.Password))
		failMsg(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		failMsg(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token := randomToken(32)
	now := time.Now()
	sess := model.Session{
		Token:     token,
		AdminID:   admin.ID,
		ExpiresAt: now.Add(sessionTTL).UnixMilli(),
		CreatedAt: now.UnixMilli(),
	}
	if err := s.db.Create(&sess).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.db.Delete(&model.Session{}, "expires_at <= ?", now.UnixMilli())

	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(sessionTTL),
	})
	c.JSON(http.StatusOK, gin.H{"username": admin.Username})
}

func (s *Server) handleLogout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		s.db.Delete(&model.Session{}, "token = ?", token)
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleMe(c *gin.Context) {
	admin := currentAdmin(c)
	c.JSON(http.StatusOK, gin.H{"username": admin.Username})
}

type changePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) handleChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.New) < 8 {
		failMsg(c, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	admin := currentAdmin(c)
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Current)); err != nil {
		failMsg(c, http.StatusUnauthorized, "current password is wrong")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	admin.PasswordHash = string(hash)
	admin.UpdatedAt = time.Now().UnixMilli()
	if err := s.db.Save(admin).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// Every other session was minted against the old password.
	s.db.Delete(&model.Session{}, "admin_id = ?", admin.ID)
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

var errNotFound = errors.New("not found")
