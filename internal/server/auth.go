package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

const (
	sessionCookie = "firex_session"
	sessionTTL    = 7 * 24 * time.Hour
	ctxAdminKey   = "firex_admin"
)

// BootstrapAdmin makes sure an admin exists and applies the config's one-shot
// password override. The password in the file is consumed: on first start it
// becomes the new admin's, on any later start it replaces the named admin's,
// and in both cases it is then blanked out of the file at configPath so a
// plaintext copy is not left to be applied again on every start. An operator
// locked out therefore gets back in by writing adminPassword and restarting.
func BootstrapAdmin(db *store.DB, cfg *config.Config, configPath string) error {
	created, generated, err := ensureAdmin(db, cfg.AdminUser, cfg.AdminPassword)
	if err != nil {
		return err
	}
	switch {
	case created && generated != "":
		log.Printf("firex: created admin %q with generated password: %s", cfg.AdminUser, generated)
		log.Printf("firex: this password is shown once; change it after signing in")
		return nil
	case created:
		log.Printf("firex: created admin %q with the password in %s", cfg.AdminUser, configPath)
	case cfg.AdminPassword == "":
		return nil
	default:
		err := resetAdminPassword(db, cfg.AdminUser, cfg.AdminPassword)
		if errors.Is(err, errNoSuchAdmin) {
			// Left in the file so that fixing adminUser and restarting is
			// enough; the operator wrote it on purpose.
			log.Printf("firex: adminPassword in %s not applied: %v", configPath, err)
			return nil
		}
		if err != nil {
			return err
		}
		log.Printf("firex: reset the password of admin %q from %s and signed out its sessions", cfg.AdminUser, configPath)
	}
	cfg.AdminPassword = ""
	// Edit the file form so a derived dbPath remains empty on disk.
	fileCfg, err := config.Read(configPath)
	if err == nil {
		fileCfg.AdminPassword = ""
		err = fileCfg.Save(configPath)
	}
	if err != nil {
		log.Printf("firex: could not blank adminPassword in %s: %v; remove it by hand, or it is applied again on every start", configPath, err)
	}
	return nil
}

// ensureAdmin creates the bootstrap admin on first run. A blank password means
// one is generated and returned so it can be printed once to the operator.
func ensureAdmin(db *store.DB, username, password string) (created bool, generated string, err error) {
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

var errNoSuchAdmin = errors.New("no such admin")

// resetAdminPassword replaces the named admin's password and signs out every
// session it holds, which were all minted against the old one.
func resetAdminPassword(db *store.DB, username, password string) error {
	var admin model.Admin
	if err := db.First(&admin, "username = ?", username).Error; err != nil {
		if !store.IsNotFound(err) {
			return err
		}
		// Naming the admin that does exist turns a puzzling refusal into a
		// one-line fix.
		var existing model.Admin
		if db.First(&existing).Error == nil {
			return fmt.Errorf("%w %q; the admin is %q", errNoSuchAdmin, username, existing.Username)
		}
		return fmt.Errorf("%w %q", errNoSuchAdmin, username)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin.PasswordHash = string(hash)
	admin.UpdatedAt = time.Now().UnixMilli()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&admin).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Session{}, "admin_id = ?", admin.ID).Error
	})
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
