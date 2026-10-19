package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/provision"
)

// Admin management is super-admin only: an operator can sign in and manage
// users but can never mint peers or promote themselves. The one rule that is
// not about roles is that the install must always keep a super admin, which is
// why demoting or deleting the last one is refused.

type adminRow struct {
	ID        uint   `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	// IsSelf lets the UI disable the actions that would lock the caller out.
	IsSelf bool `json:"isSelf"`
}

func (s *Server) listAdmins(c *gin.Context) {
	var admins []model.Admin
	if err := s.db.Order("id ASC").Find(&admins).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	self := currentAdmin(c)
	out := make([]adminRow, 0, len(admins))
	for _, admin := range admins {
		out = append(out, adminRow{
			ID: admin.ID, Username: admin.Username, Role: admin.Role,
			CreatedAt: admin.CreatedAt, UpdatedAt: admin.UpdatedAt,
			IsSelf: self != nil && self.ID == admin.ID,
		})
	}
	c.JSON(http.StatusOK, out)
}

type adminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// normalizeAdminRole keeps unknown roles from creating an admin who can do
// nothing, and an empty one an operator.
func normalizeAdminRole(role string) (string, bool) {
	switch strings.TrimSpace(role) {
	case "", model.AdminRoleOperator:
		return model.AdminRoleOperator, true
	case model.AdminRoleSuperAdmin:
		return model.AdminRoleSuperAdmin, true
	}
	return "", false
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func (s *Server) createAdmin(c *gin.Context) {
	var req adminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if err := validateUsername(username); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.Password) < 8 {
		failMsg(c, http.StatusBadRequest, "密码至少需要 8 位")
		return
	}
	role, ok := normalizeAdminRole(req.Role)
	if !ok {
		failMsg(c, http.StatusBadRequest, "无效的角色")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	now := provision.NowMs()
	admin := model.Admin{
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&admin).Error; err != nil {
		failMsg(c, http.StatusBadRequest, "用户名已被占用")
		return
	}
	c.JSON(http.StatusOK, adminRow{
		ID: admin.ID, Username: admin.Username, Role: admin.Role,
		CreatedAt: admin.CreatedAt, UpdatedAt: admin.UpdatedAt,
	})
}

func (s *Server) updateAdmin(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req adminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var admin model.Admin
	if err := s.db.First(&admin, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "管理员不存在")
		return
	}
	role, ok := normalizeAdminRole(req.Role)
	if !ok {
		failMsg(c, http.StatusBadRequest, "无效的角色")
		return
	}
	if username := strings.TrimSpace(req.Username); username != "" && username != admin.Username {
		if err := validateUsername(username); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		admin.Username = username
	}
	// Losing the last super admin would leave nobody able to manage the
	// install, and no session could restore it.
	if admin.Role == model.AdminRoleSuperAdmin && role != model.AdminRoleSuperAdmin {
		if !s.anotherSuperAdminExists(c, admin.ID) {
			failMsg(c, http.StatusConflict, "至少要保留一个超级管理员")
			return
		}
	}
	admin.Role = role
	admin.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&admin).Error; err != nil {
		failMsg(c, http.StatusBadRequest, "用户名已被占用")
		return
	}
	c.JSON(http.StatusOK, adminRow{ID: admin.ID, Username: admin.Username, Role: admin.Role, CreatedAt: admin.CreatedAt, UpdatedAt: admin.UpdatedAt})
}

func (s *Server) deleteAdmin(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var admin model.Admin
	if err := s.db.First(&admin, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "管理员不存在")
		return
	}
	if self := currentAdmin(c); self != nil && self.ID == admin.ID {
		failMsg(c, http.StatusConflict, "不能删除当前登录的管理员")
		return
	}
	if admin.Role == model.AdminRoleSuperAdmin && !s.anotherSuperAdminExists(c, admin.ID) {
		failMsg(c, http.StatusConflict, "至少要保留一个超级管理员")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.Session{}, "admin_id = ?", admin.ID).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Admin{}, admin.ID).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// s.anotherSuperAdminExists reports whether a super admin other than excludeID
// remains. The caller has already checked that excludeID is one.
func (s *Server) anotherSuperAdminExists(c *gin.Context, excludeID uint) bool {
	var count int64
	s.db.Model(&model.Admin{}).
		Where("role = ? AND id <> ?", model.AdminRoleSuperAdmin, excludeID).
		Count(&count)
	return count > 0
}

type resetAdminPasswordRequest struct {
	Password string `json:"password"`
}

// resetAdminPasswordHandler lets a super admin set another admin's password,
// which is how a locked-out operator gets back in. It is separate from
// /auth/password because that endpoint demands the current password.
func (s *Server) resetAdminPasswordHandler(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req resetAdminPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.Password) < 8 {
		failMsg(c, http.StatusBadRequest, "密码至少需要 8 位")
		return
	}
	var admin model.Admin
	if err := s.db.First(&admin, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "管理员不存在")
		return
	}
	if admin.ID == currentAdmin(c).ID {
		failMsg(c, http.StatusBadRequest, "修改自己的密码请到「设置 → 账号安全」")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	admin.PasswordHash = hash
	admin.UpdatedAt = provision.NowMs()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&admin).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Session{}, "admin_id = ?", admin.ID).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
