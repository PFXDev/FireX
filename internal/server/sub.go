package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/subscription"
)

// subTimeout bounds how long a client waits while FireX collects links from
// every panel it is provisioned on.
const subTimeout = 25 * time.Second

const (
	targetMihomo = "mihomo"
	targetBase64 = "base64"
)

// base64Agents are legacy clients that cannot consume a mihomo profile. Every
// other client gets mihomo by default; base64 remains available explicitly via
// ?target=base64.
var base64Agents = []string{"v2rayn"} // Matches both v2rayN and v2rayNG.

func (s *Server) handleSubscription(c *gin.Context) {
	token := c.Param("token")
	var u model.User
	if err := s.db.First(&u, "sub_token = ?", token).Error; err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}

	target, ok := subscriptionTarget(c.Query("target"), c.GetHeader("User-Agent"))
	if !ok {
		c.String(http.StatusBadRequest, "unsupported subscription target")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), subTimeout)
	defer cancel()
	result, err := s.subs.Build(ctx, &u)
	if err != nil {
		c.String(http.StatusBadGateway, "subscription unavailable")
		return
	}

	s.recordFetch(&u, c.GetHeader("User-Agent"))

	c.Header("Subscription-Userinfo", subscription.UserInfo(&u))
	c.Header("Profile-Update-Interval", "12")
	c.Header("Cache-Control", "no-store")
	filename := u.Username
	if target == targetMihomo {
		filename += ".yaml"
	} else {
		filename += ".txt"
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))

	if target == targetMihomo {
		yaml, err := s.subs.Clash(result, s.subBase(c))
		if err != nil {
			c.String(http.StatusInternalServerError, "template render failed: %v", err)
			return
		}
		c.Header("Profile-Title", u.Username)
		c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(yaml))
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(subscription.Base64(result)))
}

// handleRuleSet serves one rule set as the plain-text classical payload a
// mihomo rule-provider reads. Any subscription token may fetch any set: the
// lists are shared by every profile, and the token only keeps them off the
// open internet.
func (s *Server) handleRuleSet(c *gin.Context) {
	var count int64
	if err := s.db.Model(&model.User{}).Where("sub_token = ?", c.Param("token")).Count(&count).Error; err != nil || count == 0 {
		c.String(http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	var set model.RuleSet
	if err := s.db.First(&set, id).Error; err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	lines, err := routing.RuleSetLines(set.Payload)
	if err != nil {
		c.String(http.StatusInternalServerError, "rule set invalid: %v", err)
		return
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func detectTarget(userAgent string) string {
	ua := strings.ToLower(userAgent)
	for _, marker := range base64Agents {
		if strings.Contains(ua, marker) {
			return targetBase64
		}
	}
	return targetMihomo
}

// subscriptionTarget accepts both the product name (mihomo) and its familiar
// Clash compatibility name. sing-box is intentionally not an output target;
// unsupported targets fail closed instead of silently receiving another
// format.
func subscriptionTarget(requested, userAgent string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "":
		return detectTarget(userAgent), true
	case "clash", targetMihomo:
		return targetMihomo, true
	case targetBase64:
		return targetBase64, true
	default:
		return "", false
	}
}

// recordFetch is best-effort telemetry for the admin UI; a write failure must
// not cost the client its subscription.
func (s *Server) recordFetch(u *model.User, userAgent string) {
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	s.db.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{
		"last_sub_at": nowMs(),
		"last_sub_ua": userAgent,
	})
}
