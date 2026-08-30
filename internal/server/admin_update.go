package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/updater"
	"github.com/PFXDev/FireX/internal/version"
)

// handleVersion reports the running build plus the update settings, so the UI
// can say where an update would come from without a second request.
func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":       version.Version,
		"commit":        version.Commit,
		"buildTime":     version.BuildTime,
		"updateEnabled": s.cfg.Update.Enabled,
		"updateChannel": s.cfg.Update.Channel,
		"updateSource":  s.cfg.Update.Source,
		"updateRepo":    s.cfg.Update.Repo,
	})
}

// updateStatusPayload restates updater.Status in the camelCase the rest of the
// FireX API uses; the snake_case tags on updater.Status belong to version.json,
// which is a contract with the release workflow rather than with the browser.
func updateStatusPayload(st updater.Status) gin.H {
	return gin.H{
		"state":            st.State,
		"currentVersion":   st.CurrentVersion,
		"latestVersion":    st.LatestVersion,
		"isPrerelease":     st.IsPrerelease,
		"progress":         st.Progress,
		"downloadProgress": st.DownloadProgress,
		"error":            st.Error,
		"lastCheck":        st.LastCheck,
		"releaseNotes":     st.ReleaseNotes,
	}
}

func (s *Server) handleUpdateStatus(c *gin.Context) {
	c.JSON(http.StatusOK, updateStatusPayload(s.upd.Status()))
}

// handleUpdateCheck looks for a newer release without downloading it. The
// request context is used deliberately: the caller is waiting for the answer,
// so a client that walks away has nothing left to receive.
func (s *Server) handleUpdateCheck(c *gin.Context) {
	result, err := s.upd.CheckOnly(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"hasUpdate":      result.HasUpdate,
		"currentVersion": result.CurrentVersion,
		"latestVersion":  result.LatestVersion,
		"isPrerelease":   result.IsPrerelease,
		"releaseNotes":   result.ReleaseNotes,
		"channel":        result.Channel,
	})
}

// handleUpdateApply installs an already-downloaded pre-release, or starts a
// full check-download-apply pass. Both run in the background on the updater's
// own context, because the restart outlives this response.
func (s *Server) handleUpdateApply(c *gin.Context) {
	if s.upd.Status().State == "ready" {
		if err := s.upd.ApplyPending(c.Request.Context()); err != nil {
			fail(c, http.StatusConflict, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"started": true, "applyingPending": true})
		return
	}
	s.upd.StartUpdate(c.Request.Context())
	c.JSON(http.StatusAccepted, gin.H{"started": true, "applyingPending": false})
}

func (s *Server) handleUpdateDismiss(c *gin.Context) {
	s.upd.DismissPending()
	c.JSON(http.StatusOK, updateStatusPayload(s.upd.Status()))
}
