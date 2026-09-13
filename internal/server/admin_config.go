package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/model"
)

func configRevision(cfg *config.Config) string {
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Server) configPayload(cfg *config.Config) gin.H {
	redacted := *cfg
	redacted.AdminPassword = ""
	return gin.H{
		"config":                  redacted,
		"path":                    s.configPath,
		"revision":                configRevision(cfg),
		"restartRequired":         cfg.RequiresRestart(s.cfg),
		"hasPendingAdminPassword": cfg.AdminPassword != "",
	}
}

func (s *Server) getServerConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s.configMu.Lock()
	defer s.configMu.Unlock()
	cfg, err := config.Read(s.configPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, s.configPayload(cfg))
}

func (s *Server) setServerConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var req struct {
		Config   json.RawMessage `json:"config"`
		Revision string          `json:"revision"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if decoder.Decode(new(any)) != io.EOF || len(bytes.TrimSpace(req.Config)) == 0 || bytes.TrimSpace(req.Config)[0] != '{' {
		failMsg(c, http.StatusBadRequest, "config 必须是一个 JSON 对象。")
		return
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current, err := config.Read(s.configPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if req.Revision == "" || req.Revision != configRevision(current) {
		failMsg(c, http.StatusConflict, "配置已被修改，请重新加载后再保存。")
		return
	}
	// Overlay only mentioned fields, so an omitted password stays write-only.
	// Explicitly sending an empty password cancels a pending reset.
	next := *current
	decoder = json.NewDecoder(bytes.NewReader(req.Config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil {
		fail(c, http.StatusBadRequest, fmt.Errorf("配置格式错误：%w", err))
		return
	}
	fields := next.Validate()
	if next.AdminPassword != "" {
		var count int64
		if err := s.db.Model(&model.Admin{}).Where("username = ?", next.AdminUser).Count(&count).Error; err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		if count == 0 {
			fields["adminUser"] = "密码重置目标必须是已有管理员；此设置不会重命名账号。"
		}
	}
	if len(fields) > 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请修正配置中的无效值。", "fields": fields})
		return
	}
	if err := next.Save(s.configPath); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// Keep s.cfg immutable: listeners, database handles and background loops
	// all continue using the startup snapshot until the next process start.
	c.JSON(http.StatusOK, s.configPayload(&next))
}
