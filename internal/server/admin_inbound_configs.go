package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// FireX owns one panel configuration per managed inbound. Discovery only
// reports drift; an explicit deployment writes the owned configuration back,
// recreating the panel inbound or rebinding to a renumbered one if needed.

const (
	configSynced  = "synced"
	configPending = "pending"
	configDrifted = "drifted"
	configMissing = "missing"
)

func configState(inbound model.Inbound, managed *model.InboundConfig) string {
	switch {
	case managed == nil:
		return ""
	case inbound.Missing:
		return configMissing
	case managed.AppliedRevision != managed.Revision:
		return configPending
	case !panel.SameConfiguration(managed.Applied, inbound.Config):
		return configDrifted
	}
	return configSynced
}

type deploymentRow struct {
	model.InboundDeployment
	HasBackup bool `json:"hasBackup"`
}

func (s *Server) inboundConfigResponse(c *gin.Context, inbound model.Inbound) {
	var managed model.InboundConfig
	err := s.db.Where("inbound_id = ?", inbound.ID).First(&managed).Error
	if err != nil && !store.IsNotFound(err) {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var records []model.InboundDeployment
	if err := s.db.Where("inbound_id = ?", inbound.ID).Order("id DESC").Limit(30).Find(&records).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	deployments := make([]deploymentRow, 0, len(records))
	for _, record := range records {
		deployments = append(deployments, deploymentRow{record, record.BeforeConfig != ""})
	}
	response := gin.H{"config": nil, "parameters": nil, "state": "", "deployments": deployments}
	if err == nil {
		var parameters any
		if err := json.Unmarshal([]byte(managed.Config), &parameters); err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		response["config"], response["parameters"], response["state"] = managed, parameters, configState(inbound, &managed)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (s *Server) loadInbound(c *gin.Context) (model.Inbound, bool) {
	var inbound model.Inbound
	id, ok := paramID(c)
	if !ok {
		return inbound, false
	}
	if err := s.db.First(&inbound, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "入站不存在")
		return inbound, false
	}
	return inbound, true
}

func (s *Server) getInboundConfig(c *gin.Context) {
	if inbound, ok := s.loadInbound(c); ok {
		s.inboundConfigResponse(c, inbound)
	}
}

// deploymentBackup returns the before image of one of this inbound's
// deployments, so an overwritten panel can be restored from history.
func (s *Server) deploymentBackup(inboundID, deploymentID uint) (map[string]any, error) {
	var record model.InboundDeployment
	if err := s.db.First(&record, deploymentID).Error; err != nil || record.InboundID != inboundID || record.BeforeConfig == "" {
		return nil, fmt.Errorf("该下发记录没有本入站的备份")
	}
	return panel.EditableConfiguration(record.BeforeConfig)
}

// adoptInboundConfig takes ownership of the last discovered configuration, or
// of a deployment backup when the panel no longer has a trustworthy copy.
func (s *Server) adoptInboundConfig(c *gin.Context) {
	inbound, ok := s.loadInbound(c)
	if !ok {
		return
	}
	var req struct {
		DeploymentID uint `json:"deploymentId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var count int64
	s.db.Model(&model.InboundConfig{}).Where("inbound_id = ?", inbound.ID).Count(&count)
	if count > 0 {
		failMsg(c, http.StatusConflict, "入站配置已接管")
		return
	}
	var config map[string]any
	var err error
	if req.DeploymentID != 0 {
		config, err = s.deploymentBackup(inbound.ID, req.DeploymentID)
	} else if inbound.Missing || inbound.Config == "" {
		err = fmt.Errorf("入站已在面板消失，请从下发记录的备份接管")
	} else {
		config, err = panel.EditableConfiguration(inbound.Config)
	}
	if err == nil {
		err = panel.ValidateConfiguration(config)
	}
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	encoded, _ := json.Marshal(config)
	row := model.InboundConfig{InboundID: inbound.ID, Config: string(encoded), Revision: 1}
	if req.DeploymentID == 0 {
		// The panel already runs exactly this configuration.
		row.Applied, row.AppliedRevision = row.Config, 1
	}
	if err := s.db.Create(&row).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.inboundConfigResponse(c, inbound)
}

func (s *Server) updateInboundConfig(c *gin.Context) {
	inbound, ok := s.loadInbound(c)
	if !ok {
		return
	}
	var req struct {
		Revision     uint            `json:"revision"`
		Parameters   json.RawMessage `json:"parameters"`
		DeploymentID uint            `json:"deploymentId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if (len(req.Parameters) == 0) == (req.DeploymentID == 0) {
		failMsg(c, http.StatusBadRequest, "请提供参数或备份来源之一")
		return
	}
	var config map[string]any
	var err error
	if req.DeploymentID != 0 {
		config, err = s.deploymentBackup(inbound.ID, req.DeploymentID)
	} else {
		config, err = panel.EditableConfiguration(string(req.Parameters))
	}
	if err == nil {
		err = panel.ValidateConfiguration(config)
	}
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	encoded, _ := json.Marshal(config)
	result := s.db.Model(&model.InboundConfig{}).Where("inbound_id = ? AND revision = ?", inbound.ID, req.Revision).
		Updates(map[string]any{"config": string(encoded), "revision": req.Revision + 1, "updated_at": provision.NowMs()})
	if result.Error != nil {
		fail(c, http.StatusInternalServerError, result.Error)
		return
	}
	if result.RowsAffected != 1 {
		failMsg(c, http.StatusConflict, "配置已被修改或尚未接管，请刷新后重试")
		return
	}
	s.inboundConfigResponse(c, inbound)
}

// releaseInboundConfig stops managing the inbound. The panel is untouched and
// the deployment history is kept.
func (s *Server) releaseInboundConfig(c *gin.Context) {
	inbound, ok := s.loadInbound(c)
	if !ok {
		return
	}
	if err := s.db.Where("inbound_id = ?", inbound.ID).Delete(&model.InboundConfig{}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.inboundConfigResponse(c, inbound)
}

type configDeployRequest struct {
	Revision uint `json:"revision"`
	// RebindInboundID names a local row that discovery created for this
	// inbound's renumbered panel copy. It is merged into this inbound.
	RebindInboundID uint   `json:"rebindInboundId"`
	TargetRevision  string `json:"targetRevision"`
}

type deploymentPlan struct {
	inbound        model.Inbound
	managed        model.InboundConfig
	panel          model.Panel
	duplicate      *model.Inbound
	mode           string
	existing       *panel.Inbound
	desired        map[string]any
	before         map[string]any
	targetRevision string
}

type deploymentError struct {
	status  int
	message string
}

func (e *deploymentError) Error() string           { return e.message }
func deployError(status int, message string) error { return &deploymentError{status, message} }
func failDeployment(c *gin.Context, err error) {
	var e *deploymentError
	if errors.As(err, &e) {
		failMsg(c, e.status, e.message)
	} else {
		fail(c, http.StatusBadGateway, err)
	}
}

func findInbound(inbounds []panel.Inbound, id int) *panel.Inbound {
	for i := range inbounds {
		if inbounds[i].ID == id {
			return &inbounds[i]
		}
	}
	return nil
}

func wildcardListen(listen any) bool {
	return listen == "" || listen == "0.0.0.0" || listen == "::" || listen == nil
}

// inboundConflict rejects a configuration whose port or tag another panel
// inbound already uses. skip is the inbound being replaced.
func inboundConflict(live []panel.Inbound, config map[string]any, skip int) error {
	port, _ := config["port"].(float64)
	tag, _ := config["tag"].(string)
	for _, ib := range live {
		if ib.ID == skip {
			continue
		}
		if ib.Port == int(port) && (ib.Listen == config["listen"] || wildcardListen(ib.Listen) || wildcardListen(config["listen"])) {
			return deployError(http.StatusConflict, fmt.Sprintf("端口与面板入站 #%d 冲突；如果它就是本入站的新编号，请选择重新绑定", ib.ID))
		}
		if tag != "" && tag == ib.Tag {
			return deployError(http.StatusConflict, fmt.Sprintf("tag 与面板入站 #%d 冲突", ib.ID))
		}
	}
	return nil
}

// liveRevision fingerprints the panel's inbound configurations. Clients and
// counters are excluded: a preview never shows them, and deployment re-reads
// clients while holding the panel lock.
func liveRevision(p model.Panel, inbounds []panel.Inbound) []any {
	objects := []any{p.ID, p.BaseURL}
	for _, ib := range inbounds {
		config, _ := panel.EditableConfiguration(ib.Configuration())
		objects = append(objects, ib.ID, config)
	}
	return objects
}

func (s *Server) planDeployment(ctx context.Context, id uint, req configDeployRequest) (*deploymentPlan, error) {
	plan := &deploymentPlan{}
	if err := s.db.First(&plan.inbound, id).Error; err != nil {
		return nil, deployError(http.StatusNotFound, "入站不存在")
	}
	if err := s.db.Where("inbound_id = ?", id).First(&plan.managed).Error; err != nil {
		return nil, deployError(http.StatusNotFound, "入站配置尚未接管")
	}
	if req.Revision != plan.managed.Revision {
		return nil, deployError(http.StatusConflict, "配置已被修改，请重新预览")
	}
	if err := s.db.First(&plan.panel, plan.inbound.PanelID).Error; err != nil {
		return nil, deployError(http.StatusNotFound, "面板不存在")
	}
	if !plan.panel.Enabled {
		return nil, deployError(http.StatusBadRequest, "面板已停用")
	}
	desired, err := panel.EditableConfiguration(plan.managed.Config)
	if err == nil {
		err = panel.ValidateConfiguration(desired)
	}
	if err != nil {
		return nil, deployError(http.StatusBadRequest, err.Error())
	}
	plan.desired = desired
	live, err := s.mgr.ClientFor(&plan.panel).Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	plan.existing = findInbound(live, plan.inbound.RemoteID)
	switch {
	case req.RebindInboundID != 0:
		if plan.existing != nil {
			return nil, deployError(http.StatusConflict, "原面板入站仍存在，无需重新绑定")
		}
		var duplicate model.Inbound
		if err := s.db.First(&duplicate, req.RebindInboundID).Error; err != nil || duplicate.PanelID != plan.inbound.PanelID || duplicate.ID == id {
			return nil, deployError(http.StatusBadRequest, "请选择同一面板上的另一个入站")
		}
		var groups, managed int64
		s.db.Model(&model.NodeGroupInbound{}).Where("inbound_id = ?", duplicate.ID).Count(&groups)
		s.db.Model(&model.InboundConfig{}).Where("inbound_id = ?", duplicate.ID).Count(&managed)
		if groups > 0 || managed > 0 {
			return nil, deployError(http.StatusConflict, "所选入站已加入节点组或已接管，不能合并")
		}
		if plan.existing = findInbound(live, duplicate.RemoteID); plan.existing == nil {
			return nil, deployError(http.StatusConflict, "所选面板入站已消失，请同步面板后重试")
		}
		plan.duplicate, plan.mode = &duplicate, "rebind"
	case plan.existing != nil:
		plan.mode = "update"
	default:
		plan.mode = "create"
	}
	skip := 0
	if plan.existing != nil {
		if desired["protocol"] != plan.existing.Protocol {
			return nil, deployError(http.StatusBadRequest, fmt.Sprintf("面板入站协议为 %s，与配置不同，无法保留客户端更新", plan.existing.Protocol))
		}
		if plan.before, err = panel.EditableConfiguration(plan.existing.Configuration()); err != nil {
			return nil, err
		}
		skip = plan.existing.ID
	}
	if err := inboundConflict(live, desired, skip); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal([]any{liveRevision(plan.panel, live), id, plan.managed.Revision, plan.mode, req.RebindInboundID, desired})
	sum := sha256.Sum256(encoded)
	plan.targetRevision = hex.EncodeToString(sum[:])
	return plan, nil
}

type configChange struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

func configChanges(before, after map[string]any, prefix string) []configChange {
	changes := []configChange{}
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		a, aok := before[key].(map[string]any)
		b, bok := after[key].(map[string]any)
		if aok && bok {
			changes = append(changes, configChanges(a, b, path)...)
			continue
		}
		if !reflect.DeepEqual(before[key], after[key]) {
			changes = append(changes, configChange{path, before[key], after[key]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func (s *Server) previewInboundConfig(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req configDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), discoverTimeout)
	defer cancel()
	plan, err := s.planDeployment(ctx, id, req)
	if err != nil {
		failDeployment(c, err)
		return
	}
	remoteID := 0
	if plan.existing != nil {
		remoteID = plan.existing.ID
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"mode": plan.mode, "panelName": plan.panel.Name, "baseUrl": plan.panel.BaseURL, "remoteId": remoteID,
		"changes": configChanges(plan.before, plan.desired, ""), "warnings": panel.FallbackWarnings(plan.desired),
		"targetRevision": plan.targetRevision,
	})
}

func (s *Server) deployInboundConfig(c *gin.Context) {
	inbound, ok := s.loadInbound(c)
	if !ok {
		return
	}
	var req configDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if req.TargetRevision == "" {
		failMsg(c, http.StatusBadRequest, "请先预览下发差异")
		return
	}
	record, err := s.runDeployment(inbound, req)
	if err != nil {
		failDeployment(c, err)
		return
	}
	var syncErr error
	if record.Status == "verified" && record.Mode != "update" {
		// A recreated or rebound panel inbound lacks FireX's users until pushed.
		syncErr = s.reconcileInbound(inbound.ID)
	}
	s.subs.InvalidateAll()
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"deployment": record, "syncError": errString(syncErr)})
}

// runDeployment holds the panel lock from the fresh read to the binding, so
// discovery and user reconciliation cannot interleave with the write.
func (s *Server) runDeployment(inbound model.Inbound, req configDeployRequest) (*model.InboundDeployment, error) {
	defer s.mgr.PanelWrite(inbound.PanelID)()
	ctx, cancel := opCtx()
	defer cancel()
	plan, err := s.planDeployment(ctx, inbound.ID, req)
	if err != nil {
		return nil, err
	}
	if req.TargetRevision != plan.targetRevision {
		return nil, deployError(http.StatusConflict, "面板或接管配置已变化，请重新预览")
	}
	desired, _ := json.Marshal(plan.desired)
	record := model.InboundDeployment{InboundID: inbound.ID, Revision: plan.managed.Revision, PanelID: inbound.PanelID, Mode: plan.mode, Status: "pending", DesiredConfig: string(desired)}
	if plan.existing != nil {
		before, _ := json.Marshal(plan.before)
		record.RemoteID, record.BeforeConfig = plan.existing.ID, string(before)
	}
	if err := s.db.Create(&record).Error; err != nil {
		return nil, deployError(http.StatusInternalServerError, err.Error())
	}
	found, actual, writeErr := s.pushInbound(ctx, &plan.panel, plan.desired, plan.existing)
	if found != nil {
		// Bind even when verification failed: a retry must update this panel
		// inbound rather than create another one.
		record.RemoteID = found.ID
		if err := s.bindInbound(plan, found, actual, writeErr == nil); err != nil && writeErr == nil {
			writeErr = err
		}
	}
	s.finishDeployment(&record, writeErr)
	return &record, nil
}

// pushInbound sends one write and reads the panel back. found is the written
// inbound whenever the panel reports it, even if verification fails.
func (s *Server) pushInbound(ctx context.Context, p *model.Panel, desired map[string]any, existing *panel.Inbound) (*panel.Inbound, map[string]any, error) {
	client := s.mgr.ClientFor(p)
	result, err := client.WriteInbound(ctx, desired, existing)
	if err != nil {
		return nil, nil, err
	}
	enable, _ := desired["enable"].(bool)
	var found *panel.Inbound
	for attempt := 0; ; attempt++ {
		live, err := client.Inbounds(ctx)
		if err != nil {
			return nil, nil, err
		}
		if found = findInbound(live, result.ID); found == nil {
			return nil, nil, fmt.Errorf("写入后未找到面板入站 #%d，请同步面板后核对", result.ID)
		}
		if found.Enable == enable || attempt > 0 {
			break
		}
		// Modern panels ignore enable in an update and expose a separate switch.
		if err := client.SetInboundEnabled(ctx, found.ID, enable); err != nil {
			return found, nil, err
		}
	}
	actual, err := panel.EditableConfiguration(found.Configuration())
	if err != nil {
		return found, nil, err
	}
	if path := panel.MismatchedPath(desired, actual); path != "" {
		return found, actual, fmt.Errorf("面板读回的 %s 与接管配置不同，请核对", path)
	}
	if existing != nil {
		var before, after map[string]any
		_ = json.Unmarshal([]byte(existing.Settings), &before)
		_ = json.Unmarshal([]byte(found.Settings), &after)
		if !reflect.DeepEqual(before["clients"], after["clients"]) {
			return found, actual, fmt.Errorf("面板入站的客户端发生变化，请核对")
		}
	}
	return found, actual, nil
}

// bindInbound points the local inbound at the panel inbound just written,
// keeping its FireX identity, groups and users. A verified readback becomes
// the baseline for drift detection.
func (s *Server) bindInbound(plan *deploymentPlan, found *panel.Inbound, actual map[string]any, verified bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if plan.duplicate != nil {
			if err := tx.Delete(&model.Inbound{}, plan.duplicate.ID).Error; err != nil {
				return err
			}
		}
		var other model.Inbound
		err := tx.Where("panel_id = ? AND remote_id = ? AND id <> ?", plan.inbound.PanelID, found.ID, plan.inbound.ID).First(&other).Error
		if err == nil {
			return fmt.Errorf("面板入站 #%d 已对应本地入站 #%d，请核对", found.ID, other.ID)
		}
		if !store.IsNotFound(err) {
			return err
		}
		inbound := plan.inbound
		provision.ApplyDiscovered(&inbound, found, provision.NowMs())
		if err := tx.Save(&inbound).Error; err != nil {
			return err
		}
		if !verified {
			return nil
		}
		applied, _ := json.Marshal(actual)
		return tx.Model(&model.InboundConfig{}).Where("inbound_id = ? AND revision = ?", inbound.ID, plan.managed.Revision).
			Updates(map[string]any{"applied": string(applied), "applied_revision": plan.managed.Revision}).Error
	})
}

func (s *Server) finishDeployment(record *model.InboundDeployment, cause error) {
	record.Status = "verified"
	if cause != nil {
		record.Status, record.Error = "unverified", cause.Error()
	}
	if err := s.db.Model(record).Select("status", "error", "inbound_id", "remote_id").Updates(record).Error; err != nil && cause == nil {
		record.Status, record.Error = "unverified", "保存下发记录失败："+err.Error()
	}
}
