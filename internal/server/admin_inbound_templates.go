package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Templates carry the shared part of an inbound, such as a REALITY setup.
// Creating an inbound from one adds node-specific fields and hands the result
// to FireX as a managed configuration; later template edits do not touch it.

type templateResponse struct {
	Template   model.InboundTemplate `json:"template"`
	Parameters map[string]any        `json:"parameters"`
}

func (s *Server) listInboundTemplates(c *gin.Context) {
	var rows []model.InboundTemplate
	if err := s.db.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, rows)
}

func templateName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && len(name) <= 200
}

func templateParameters(raw string) (map[string]any, error) {
	config, err := panel.EditableConfiguration(raw)
	if err != nil {
		return nil, err
	}
	config = panel.TemplateConfiguration(config)
	return config, panel.ValidateTemplate(config)
}

// createInboundTemplate copies an inbound's managed configuration, or its last
// discovered one when it is not managed, without node-specific fields.
func (s *Server) createInboundTemplate(c *gin.Context) {
	var req struct {
		Name       string          `json:"name"`
		InboundID  uint            `json:"inboundId"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	name, ok := templateName(req.Name)
	if !ok {
		failMsg(c, http.StatusBadRequest, "模板名称不能为空且不能超过 200 字节")
		return
	}
	if (req.InboundID == 0) == (len(req.Parameters) == 0) {
		failMsg(c, http.StatusBadRequest, "请选择一个模板来源")
		return
	}
	raw := string(req.Parameters)
	if req.InboundID != 0 {
		var inbound model.Inbound
		if err := s.db.First(&inbound, req.InboundID).Error; err != nil {
			failMsg(c, http.StatusNotFound, "入站不存在")
			return
		}
		raw = inbound.Config
		var managed model.InboundConfig
		if s.db.Where("inbound_id = ?", inbound.ID).First(&managed).Error == nil {
			raw = managed.Config
		}
	}
	config, err := templateParameters(raw)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	encoded, _ := json.Marshal(config)
	row := model.InboundTemplate{Name: name, Config: string(encoded), Revision: 1}
	if err := s.db.Create(&row).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, templateResponse{row, config})
}

func (s *Server) loadTemplate(c *gin.Context) (model.InboundTemplate, bool) {
	var row model.InboundTemplate
	id, ok := paramID(c)
	if !ok {
		return row, false
	}
	if err := s.db.First(&row, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "模板不存在")
		return row, false
	}
	return row, true
}

func (s *Server) getInboundTemplate(c *gin.Context) {
	row, ok := s.loadTemplate(c)
	if !ok {
		return
	}
	var parameters map[string]any
	if err := json.Unmarshal([]byte(row.Config), &parameters); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, templateResponse{row, parameters})
}

func (s *Server) updateInboundTemplate(c *gin.Context) {
	row, ok := s.loadTemplate(c)
	if !ok {
		return
	}
	var req struct {
		Revision   uint            `json:"revision"`
		Name       *string         `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	updates := map[string]any{"revision": req.Revision + 1, "updated_at": provision.NowMs()}
	if req.Name != nil {
		name, ok := templateName(*req.Name)
		if !ok {
			failMsg(c, http.StatusBadRequest, "模板名称不能为空且不能超过 200 字节")
			return
		}
		updates["name"] = name
	}
	if len(req.Parameters) > 0 {
		config, err := templateParameters(string(req.Parameters))
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		encoded, _ := json.Marshal(config)
		updates["config"] = string(encoded)
	}
	result := s.db.Model(&model.InboundTemplate{}).Where("id = ? AND revision = ?", row.ID, req.Revision).Updates(updates)
	if result.Error != nil {
		fail(c, http.StatusInternalServerError, result.Error)
		return
	}
	if result.RowsAffected != 1 {
		failMsg(c, http.StatusConflict, "模板已被修改，请刷新后重试")
		return
	}
	s.getInboundTemplate(c)
}

func (s *Server) deleteInboundTemplate(c *gin.Context) {
	row, ok := s.loadTemplate(c)
	if !ok {
		return
	}
	if err := s.db.Delete(&model.InboundTemplate{}, row.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type instantiateRequest struct {
	panel.NodeEnvironment
	Revision      uint   `json:"revision"`
	PanelID       uint   `json:"panelId"`
	PublicAddress string `json:"publicAddress"`
	PublicPort    int    `json:"publicPort"`
}

// instantiateInboundTemplate creates a panel inbound and a managed local row
// for it. Like any discovered inbound it stays disabled and ungrouped.
func (s *Server) instantiateInboundTemplate(c *gin.Context) {
	row, ok := s.loadTemplate(c)
	if !ok {
		return
	}
	var req instantiateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if req.Revision != row.Revision {
		failMsg(c, http.StatusConflict, "模板已被修改，请刷新后重试")
		return
	}
	var target model.Panel
	if err := s.db.First(&target, req.PanelID).Error; err != nil {
		failMsg(c, http.StatusNotFound, "面板不存在")
		return
	}
	if !target.Enabled {
		failMsg(c, http.StatusBadRequest, "面板已停用")
		return
	}
	req.PublicAddress = strings.TrimSpace(req.PublicAddress)
	if !validPublicAddress(req.PublicAddress) || req.PublicPort < 0 || req.PublicPort > 65535 {
		failMsg(c, http.StatusBadRequest, "客户端地址或端口无效")
		return
	}
	template, err := panel.EditableConfiguration(row.Config)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	desired := panel.WithNode(template, req.NodeEnvironment)
	// The address belongs on the panel so later edits there reach every
	// subscription; FireX only keeps it when the panel cannot store it.
	if req.PublicAddress != "" {
		desired["shareAddrStrategy"], desired["shareAddr"] = "custom", req.PublicAddress
	}
	if err := panel.ValidateConfiguration(desired); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	record, err := s.runInstantiate(row, target, desired, req)
	if err != nil {
		failDeployment(c, err)
		return
	}
	s.subs.InvalidateAll()
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"deployment": record})
}

func (s *Server) runInstantiate(row model.InboundTemplate, target model.Panel, desired map[string]any, req instantiateRequest) (*model.InboundDeployment, error) {
	defer s.mgr.PanelWrite(target.ID)()
	ctx, cancel := opCtx()
	defer cancel()
	live, err := s.mgr.ClientFor(&target).Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	if err := inboundConflict(live, desired, 0); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(desired)
	record := model.InboundDeployment{TemplateID: row.ID, Revision: row.Revision, PanelID: target.ID, Mode: "create", Status: "pending", DesiredConfig: string(encoded)}
	if err := s.db.Create(&record).Error; err != nil {
		return nil, deployError(http.StatusInternalServerError, err.Error())
	}
	found, actual, writeErr := s.pushInbound(ctx, &target, desired, nil)
	if found != nil {
		record.RemoteID = found.ID
		var inboundID uint
		err := s.db.Transaction(func(tx *gorm.DB) error {
			inbound := provision.NewInbound(target.ID, found, provision.NowMs())
			inbound.PublicPort = req.PublicPort
			if panel.ShareAddress(inbound.Config) != req.PublicAddress {
				inbound.PublicAddress = req.PublicAddress
			}
			if err := tx.Create(&inbound).Error; err != nil {
				return err
			}
			managed := model.InboundConfig{InboundID: inbound.ID, Config: string(encoded), Revision: 1}
			if writeErr == nil {
				applied, _ := json.Marshal(actual)
				managed.Applied, managed.AppliedRevision = string(applied), 1
			}
			inboundID = inbound.ID
			return tx.Create(&managed).Error
		})
		if err == nil {
			record.InboundID = inboundID
		} else if writeErr == nil {
			writeErr = err
		}
	}
	s.finishDeployment(&record, writeErr)
	return &record, nil
}
