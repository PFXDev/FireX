package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/subscription"
)

// subTimeout bounds how long a client waits while FireX collects links from
// every panel it is provisioned on.
const subTimeout = 25 * time.Second

// clashAgents are the user-agent markers of clients that want a mihomo profile
// rather than a base64 link list.
var clashAgents = []string{"clash", "mihomo", "meta", "stash", "clashx", "flclash", "shadowrocket-clash"}

func (s *Server) handleSubscription(c *gin.Context) {
	token := c.Param("token")
	var u model.User
	if err := s.db.First(&u, "sub_token = ?", token).Error; err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}

	target := strings.ToLower(strings.TrimSpace(c.Query("target")))
	if target == "" {
		target = detectTarget(c.GetHeader("User-Agent"))
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
	if target == "clash" {
		filename += ".yaml"
	} else {
		filename += ".txt"
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))

	if target == "clash" {
		yaml, err := s.subs.Clash(result)
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

func detectTarget(userAgent string) string {
	ua := strings.ToLower(userAgent)
	for _, marker := range clashAgents {
		if strings.Contains(ua, marker) {
			return "clash"
		}
	}
	return "base64"
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
