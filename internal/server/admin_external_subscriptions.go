package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
)

type externalSubscriptionRequest struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Enabled        *bool  `json:"enabled"`
	UpdateInterval *int   `json:"updateInterval"`
	Remark         string `json:"remark"`
}

func (req *externalSubscriptionRequest) apply(sub *model.ExternalSubscription) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errBadRequest("外部订阅需要一个名称")
	}
	validated, err := subscription.ValidateExternalSubscriptionURL(req.URL)
	if err != nil {
		return errBadRequest(err.Error())
	}
	sub.Name = name
	sub.URL = validated
	sub.Remark = req.Remark
	if req.Enabled != nil {
		sub.Enabled = *req.Enabled
	}
	if req.UpdateInterval != nil {
		if *req.UpdateInterval < subscription.MinExternalUpdateInterval {
			return errBadRequest("更新周期不能少于 60 秒")
		}
		sub.UpdateInterval = *req.UpdateInterval
	}
	return nil
}

type externalSubscriptionRow struct {
	model.ExternalSubscription
	URL        string `json:"url"`
	NodeCount  int    `json:"nodeCount"`
	GroupCount int    `json:"groupCount"`
}

func (s *Server) externalSubscriptionView(c *gin.Context, sub model.ExternalSubscription) externalSubscriptionRow {
	row := externalSubscriptionRow{
		ExternalSubscription: sub,
		URL:                  sub.URL,
		NodeCount:            subscription.ExternalNodeCount(&sub),
	}
	if !isSuperAdmin(currentAdmin(c)) {
		row.URL = redactedValue
	}
	var groups int64
	s.db.Model(&model.NodeGroupExternalSubscription{}).Where("subscription_id = ?", sub.ID).Count(&groups)
	row.GroupCount = int(groups)
	return row
}

func (s *Server) listExternalSubscriptions(c *gin.Context) {
	var subs []model.ExternalSubscription
	if err := s.db.Order("name ASC, id ASC").Find(&subs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	out := make([]externalSubscriptionRow, 0, len(subs))
	for _, sub := range subs {
		out = append(out, s.externalSubscriptionView(c, sub))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) getExternalSubscription(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var sub model.ExternalSubscription
	if err := s.db.First(&sub, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "external subscription not found")
		return
	}
	nodes, err := subscription.ExternalNodes(&sub)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	type nodeView struct {
		Name         string `json:"name"`
		Protocol     string `json:"protocol"`
		Server       string `json:"server"`
		Port         int    `json:"port"`
		HasShareLink bool   `json:"hasShareLink"`
	}
	views := make([]nodeView, 0, len(nodes))
	for _, node := range nodes {
		views = append(views, nodeView{
			Name: node.Name, Protocol: node.Protocol, Server: node.Server,
			Port: node.Port, HasShareLink: node.HasShareLink,
		})
	}
	c.JSON(http.StatusOK, gin.H{"subscription": s.externalSubscriptionView(c, sub), "nodes": views})
}

func (s *Server) createExternalSubscription(c *gin.Context) {
	var req externalSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	sub := model.ExternalSubscription{
		Enabled: true, UpdateInterval: subscription.DefaultExternalUpdateInterval,
	}
	if err := req.apply(&sub); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := createRow(s.db.DB, &sub); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := opCtx()
	refreshErr := s.subs.RefreshExternalSubscription(ctx, sub.ID)
	cancel()
	s.db.First(&sub, sub.ID)
	c.JSON(http.StatusOK, gin.H{
		"subscription": s.externalSubscriptionView(c, sub), "refreshError": errString(refreshErr),
	})
}

func (s *Server) updateExternalSubscription(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req externalSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var sub model.ExternalSubscription
	if err := s.db.First(&sub, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "external subscription not found")
		return
	}
	previousURL := sub.URL
	if err := req.apply(&sub); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	// Edit settings without writing back a stale copy of the refresh cache.
	changes := map[string]any{
		"name": sub.Name, "url": sub.URL, "enabled": sub.Enabled,
		"update_interval": sub.UpdateInterval, "remark": sub.Remark,
	}
	if previousURL != sub.URL {
		changes["last_check_at"] = 0
	}
	if err := s.db.Model(&sub).Updates(changes).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := opCtx()
	refreshErr := s.subs.RefreshExternalSubscription(ctx, sub.ID)
	cancel()
	s.db.First(&sub, sub.ID)
	c.JSON(http.StatusOK, gin.H{
		"subscription": s.externalSubscriptionView(c, sub), "refreshError": errString(refreshErr),
	})
}

func (s *Server) refreshExternalSubscription(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var sub model.ExternalSubscription
	if err := s.db.First(&sub, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "external subscription not found")
		return
	}
	ctx, cancel := opCtx()
	refreshErr := s.subs.RefreshExternalSubscription(ctx, id)
	cancel()
	if errors.Is(refreshErr, subscription.ErrRefreshRunning) {
		fail(c, http.StatusConflict, refreshErr)
		return
	}
	s.db.First(&sub, id)
	c.JSON(http.StatusOK, gin.H{
		"subscription": s.externalSubscriptionView(c, sub), "refreshError": errString(refreshErr),
	})
}

func (s *Server) deleteExternalSubscription(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var sub model.ExternalSubscription
	if err := s.db.First(&sub, id).Error; err != nil {
		if store.IsNotFound(err) {
			failMsg(c, http.StatusNotFound, "external subscription not found")
		} else {
			fail(c, http.StatusInternalServerError, err)
		}
		return
	}
	err := s.db.Transaction(func(handle *gorm.DB) error {
		if err := handle.Where("subscription_id = ?", id).Delete(&model.NodeGroupExternalSubscription{}).Error; err != nil {
			return err
		}
		return handle.Delete(&model.ExternalSubscription{}, id).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
