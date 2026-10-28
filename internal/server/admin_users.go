package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/subscription"
)

// -------------------------------------------------------------------- plans

type planRow struct {
	model.Plan
	ProfileName string `json:"profileName"`
	// UsableInbounds is what the bound profile currently grants, so a plan that
	// routes nowhere is visible without opening the profile.
	UsableInbounds int   `json:"usableInbounds"`
	UserCount      int64 `json:"userCount"`
	// PanelLimits are the plan's per-user caps on single panels.
	PanelLimits []model.PlanPanelLimit `json:"panelLimits"`
}

type planRequest struct {
	Name         string `json:"name"`
	ProfileID    *uint  `json:"profileId"`
	TrafficBytes int64  `json:"trafficBytes"`
	DurationDays int    `json:"durationDays"`
	DeviceLimit  int    `json:"deviceLimit"`
	TrafficReset string `json:"trafficReset"`
	SpeedNote    string `json:"speedNote"`
	Enabled      *bool  `json:"enabled"`
	SortOrder    *int   `json:"sortOrder"`
	Remark       string `json:"remark"`
	// PanelLimits replaces the plan's per-panel caps when present; nil keeps
	// them, so a client that predates them cannot clear them by omission.
	PanelLimits []model.PlanPanelLimit `json:"panelLimits"`
}

// validPanelLimits drops zero caps, which mean "no cap here", and rejects
// negative ones, repeated panels and panels that do not exist.
func (s *Server) validPanelLimits(c *gin.Context, limits []model.PlanPanelLimit) ([]model.PlanPanelLimit, bool) {
	out := make([]model.PlanPanelLimit, 0, len(limits))
	seen := map[uint]bool{}
	for _, l := range limits {
		if l.TrafficBytes < 0 {
			failMsg(c, http.StatusBadRequest, "panel traffic limit must not be negative")
			return nil, false
		}
		if seen[l.PanelID] {
			failMsg(c, http.StatusBadRequest, "panel listed twice in panelLimits")
			return nil, false
		}
		seen[l.PanelID] = true
		var count int64
		if s.db.Model(&model.Panel{}).Where("id = ?", l.PanelID).Count(&count); count == 0 {
			failMsg(c, http.StatusBadRequest, "panelLimits names a panel that does not exist")
			return nil, false
		}
		if l.TrafficBytes > 0 {
			out = append(out, model.PlanPanelLimit{PanelID: l.PanelID, TrafficBytes: l.TrafficBytes})
		}
	}
	return out, true
}

// savePanelLimits replaces the plan's per-panel caps.
func savePanelLimits(tx *gorm.DB, planID uint, limits []model.PlanPanelLimit) error {
	if err := tx.Where("plan_id = ?", planID).Delete(&model.PlanPanelLimit{}).Error; err != nil {
		return err
	}
	for i := range limits {
		limits[i].PlanID = planID
	}
	if len(limits) == 0 {
		return nil
	}
	return tx.Create(&limits).Error
}

func (s *Server) listPlans(c *gin.Context) {
	var plans []model.Plan
	if err := s.db.Order("sort_order ASC, id ASC").Find(&plans).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var profiles []model.Profile
	s.db.Find(&profiles)
	profileNames := map[uint]string{}
	for _, p := range profiles {
		profileNames[p.ID] = p.Name
	}
	var limits []model.PlanPanelLimit
	s.db.Order("panel_id ASC").Find(&limits)
	limitsByPlan := map[uint][]model.PlanPanelLimit{}
	for _, l := range limits {
		limitsByPlan[l.PlanID] = append(limitsByPlan[l.PlanID], l)
	}
	out := make([]planRow, 0, len(plans))
	for _, p := range plans {
		row := planRow{Plan: p, ProfileName: profileNames[p.ProfileID], PanelLimits: limitsByPlan[p.ID]}
		if row.PanelLimits == nil {
			row.PanelLimits = []model.PlanPanelLimit{}
		}
		s.db.Model(&model.User{}).Where("plan_id = ?", p.ID).Count(&row.UserCount)
		inbounds, _ := routing.InboundsForProfile(s.db, p.ProfileID)
		row.UsableInbounds = len(inbounds)
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) createPlan(c *gin.Context) {
	var req planRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		failMsg(c, http.StatusBadRequest, "name is required")
		return
	}
	reset, ok := trafficResetOf(c, req.TrafficReset, model.TrafficResetNone)
	if !ok {
		return
	}
	limits, ok := s.validPanelLimits(c, req.PanelLimits)
	if !ok {
		return
	}
	now := provision.NowMs()
	plan := model.Plan{
		Name:         strings.TrimSpace(req.Name),
		ProfileID:    valueOr(req.ProfileID, 0),
		TrafficBytes: req.TrafficBytes,
		DurationDays: req.DurationDays,
		DeviceLimit:  req.DeviceLimit,
		TrafficReset: reset,
		SpeedNote:    req.SpeedNote,
		Enabled:      req.Enabled == nil || *req.Enabled,
		SortOrder:    valueOr(req.SortOrder, 100),
		Remark:       req.Remark,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		return savePanelLimits(tx, plan.ID, limits)
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (s *Server) updatePlan(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req planRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var plan model.Plan
	if err := s.db.First(&plan, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "plan not found")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		plan.Name = strings.TrimSpace(req.Name)
	}
	if req.ProfileID != nil {
		plan.ProfileID = *req.ProfileID
	}
	reset, ok := trafficResetOf(c, req.TrafficReset, plan.TrafficReset)
	if !ok {
		return
	}
	var limits []model.PlanPanelLimit
	if req.PanelLimits != nil {
		if limits, ok = s.validPanelLimits(c, req.PanelLimits); !ok {
			return
		}
	}
	resetChanged := reset != plan.TrafficReset
	plan.TrafficReset = reset
	plan.TrafficBytes = req.TrafficBytes
	plan.DurationDays = req.DurationDays
	plan.DeviceLimit = req.DeviceLimit
	plan.SpeedNote = req.SpeedNote
	if req.Enabled != nil {
		plan.Enabled = *req.Enabled
	}
	if req.SortOrder != nil {
		plan.SortOrder = *req.SortOrder
	}
	plan.Remark = req.Remark
	plan.UpdatedAt = provision.NowMs()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&plan).Error; err != nil {
			return err
		}
		if req.PanelLimits == nil {
			return nil
		}
		return savePanelLimits(tx, plan.ID, limits)
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if resetChanged {
		// Count the new schedule from now. Measured from each user's creation
		// date instead, every long-standing user would be due at once and the
		// next poll would wipe the plan's usage wholesale.
		if err := s.db.Model(&model.User{}).Where("plan_id = ?", plan.ID).
			Update("traffic_period_start", plan.UpdatedAt).Error; err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
	}
	s.subs.InvalidateAll()

	// Switching profile changes the inbound set, and the device limit is pushed
	// to the panels too, so the plan's users go out before we answer.
	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ReconcileUsersOfPlan(ctx, plan.ID)
	c.JSON(http.StatusOK, gin.H{"plan": plan, "syncError": errString(syncErr)})
}

func (s *Server) deletePlan(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var count int64
	s.db.Model(&model.User{}).Where("plan_id = ?", id).Count(&count)
	if count > 0 {
		failMsg(c, http.StatusConflict, "plan still has users; move them to another plan first")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_id = ?", id).Delete(&model.PlanPanelLimit{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Plan{}, id).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// -------------------------------------------------------------------- users

type userRow struct {
	model.User
	PlanName string `json:"planName"`
	// TrafficReset is the plan's policy, and NextTrafficReset when it next
	// zeroes this user's usage (0 for never).
	TrafficReset     string   `json:"trafficReset"`
	NextTrafficReset int64    `json:"nextTrafficReset"`
	Used             int64    `json:"used"`
	InboundCount     int      `json:"inboundCount"`
	SyncState        string   `json:"syncState"`
	SyncError        []string `json:"syncErrors"`
	SubURL           string   `json:"subUrl"`
	// Panels is the user's usage on each panel they hold a client on this
	// period, with the plan's cap there (0 for none) and, when the panel is
	// closed to them, why.
	Panels []userPanelUsage `json:"panels"`
}

type userPanelUsage struct {
	PanelID   uint   `json:"panelId"`
	PanelName string `json:"panelName"`
	Upload    int64  `json:"upload"`
	Download  int64  `json:"download"`
	Limit     int64  `json:"limit"`
	Blocked   string `json:"blocked"`
}

type userRequest struct {
	Username     string `json:"username"`
	UUID         string `json:"uuid"`
	PlanID       uint   `json:"planId"`
	Enabled      *bool  `json:"enabled"`
	ExpiresAt    *int64 `json:"expiresAt"`
	TrafficLimit *int64 `json:"trafficLimit"`
	Remark       string `json:"remark"`
}

func (s *Server) listUsers(c *gin.Context) {
	var users []model.User
	if err := s.db.Order("id ASC").Find(&users).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	plansByID := map[uint]*model.Plan{}
	var plans []model.Plan
	s.db.Find(&plans)
	for i := range plans {
		plansByID[plans[i].ID] = &plans[i]
	}

	panelNames := map[uint]string{}
	var panels []model.Panel
	s.db.Select("id", "name").Find(&panels)
	for _, p := range panels {
		panelNames[p.ID] = p.Name
	}
	var limits []model.PlanPanelLimit
	s.db.Find(&limits)
	planLimits := map[uint]map[uint]int64{}
	for _, l := range limits {
		if planLimits[l.PlanID] == nil {
			planLimits[l.PlanID] = map[uint]int64{}
		}
		planLimits[l.PlanID][l.PanelID] = l.TrafficBytes
	}

	base := s.subBase(c)
	out := make([]userRow, 0, len(users))
	for i := range users {
		u := users[i]
		plan := plansByID[u.PlanID]
		row := userRow{
			User:             u,
			TrafficReset:     model.TrafficResetNone,
			NextTrafficReset: s.mgr.NextTrafficReset(&u, plan),
			Used:             u.Upload + u.Download,
			SyncState:        model.SyncStateSynced,
			SyncError:        []string{},
			SubURL:           base + "/sub/" + u.SubToken,
		}
		if plan != nil {
			row.PlanName, row.TrafficReset = plan.Name, resetModeOf(plan)
		}
		inbounds, _ := s.mgr.InboundsForUser(&u)
		row.InboundCount = len(inbounds)

		var records []model.UserPanel
		s.db.Where("user_id = ?", u.ID).Order("panel_id ASC").Find(&records)
		blocked, _ := s.mgr.BlockedPanels(&u)
		row.Panels = make([]userPanelUsage, 0, len(records))
		for _, rec := range records {
			row.Panels = append(row.Panels, userPanelUsage{
				PanelID: rec.PanelID, PanelName: panelNames[rec.PanelID],
				Upload: rec.Upload, Download: rec.Download,
				Limit: planLimits[u.PlanID][rec.PanelID], Blocked: blocked[rec.PanelID],
			})
		}
		if len(records) == 0 && row.InboundCount > 0 {
			row.SyncState = model.SyncStatePending
		}
		for _, rec := range records {
			if rec.State != model.SyncStateSynced {
				row.SyncState = rec.State
			}
			if rec.LastError != "" {
				row.SyncError = append(row.SyncError, rec.LastError)
			}
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) createUser(c *gin.Context) {
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if err := validateUsername(username); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	now := provision.NowMs()
	u := model.User{
		Username:  username,
		UUID:      strings.TrimSpace(req.UUID),
		SubToken:  randomToken(16),
		PlanID:    req.PlanID,
		Enabled:   req.Enabled == nil || *req.Enabled,
		Remark:    req.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if u.UUID == "" {
		u.UUID = uuid.NewString()
	}

	plan, err := s.planByID(req.PlanID)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	// Unset quota/expiry inherit the plan, which is what "put this user on the
	// 100 GB monthly plan" is expected to mean.
	if req.TrafficLimit != nil {
		u.TrafficLimit = *req.TrafficLimit
	} else if plan != nil {
		u.TrafficLimit = plan.TrafficBytes
	}
	if req.ExpiresAt != nil {
		u.ExpiresAt = *req.ExpiresAt
	} else if plan != nil && plan.DurationDays > 0 {
		u.ExpiresAt = now + int64(plan.DurationDays)*24*60*60*1000
	}

	if err := s.db.Create(&u).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ReconcileUser(ctx, &u)
	c.JSON(http.StatusOK, gin.H{
		"user":      u,
		"subUrl":    s.subBase(c) + "/sub/" + u.SubToken,
		"syncError": errString(syncErr),
	})
}

func (s *Server) updateUser(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "user not found")
		return
	}
	// The username is the panel-side email; renaming would orphan the remote
	// clients, so it is fixed once created.
	if strings.TrimSpace(req.Username) != "" && strings.TrimSpace(req.Username) != u.Username {
		failMsg(c, http.StatusBadRequest, "username cannot be changed; delete and recreate the user")
		return
	}
	if strings.TrimSpace(req.UUID) != "" {
		u.UUID = strings.TrimSpace(req.UUID)
	}
	if req.PlanID != u.PlanID {
		oldPlan, _ := s.planByID(u.PlanID)
		newPlan, err := s.planByID(req.PlanID)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		// Same reasoning as a plan changing its policy: the new schedule
		// counts from the move, not from when the user was created.
		if resetModeOf(oldPlan) != resetModeOf(newPlan) {
			u.TrafficPeriodStart = provision.NowMs()
		}
	}
	u.PlanID = req.PlanID
	if req.Enabled != nil {
		u.Enabled = *req.Enabled
	}
	if req.ExpiresAt != nil {
		u.ExpiresAt = *req.ExpiresAt
	}
	if req.TrafficLimit != nil {
		u.TrafficLimit = *req.TrafficLimit
		// Raising the cap must actually bring the user back online.
		u.Depleted = u.TrafficLimit > 0 && u.Upload+u.Download >= u.TrafficLimit
	}
	u.Remark = req.Remark
	u.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&u).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	s.subs.InvalidateUser(&u)

	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ReconcileUser(ctx, &u)
	c.JSON(http.StatusOK, gin.H{"user": u, "syncError": errString(syncErr)})
}

func (s *Server) deleteUser(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "user not found")
		return
	}
	// Clear the plan first so reconcile removes the client from every panel,
	// then drop the local rows.
	u.PlanID = 0
	u.Enabled = false
	s.db.Save(&u)

	ctx, cancel := opCtx()
	defer cancel()
	syncErr := s.mgr.ReconcileUser(ctx, &u)

	s.db.Where("user_id = ?", u.ID).Delete(&model.UserPanel{})
	if err := s.db.Delete(&model.User{}, u.ID).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.subs.InvalidateUser(&u)
	c.JSON(http.StatusOK, gin.H{"ok": true, "syncError": errString(syncErr)})
}

func (s *Server) resyncUser(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "user not found")
		return
	}
	s.subs.InvalidateUser(&u)
	ctx, cancel := opCtx()
	defer cancel()
	if err := s.mgr.ReconcileUser(ctx, &u); err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) resetUserTraffic(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "user not found")
		return
	}

	ctx, cancel := opCtx()
	defer cancel()
	// A manual reset also starts a new period, but the scheduled boundaries
	// stay where they were: a monthly user reset on the 10th still resets on
	// their usual day.
	remoteFailures, err := s.mgr.ResetTraffic(ctx, &u)
	s.subs.InvalidateUser(&u)
	c.JSON(http.StatusOK, gin.H{"ok": true, "remoteFailures": remoteFailures, "syncError": errString(err)})
}

func (s *Server) previewSubscription(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "user not found")
		return
	}
	ctx, cancel := opCtx()
	defer cancel()
	result, err := s.subs.Build(ctx, &u)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	yaml, renderErr := s.subs.Clash(result, s.subBase(c))

	type entryView struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Panel    uint   `json:"panelId"`
		Clash    bool   `json:"clashSupported"`
		Link     string `json:"link"`
	}
	views := make([]entryView, 0, len(result.Entries))
	for _, e := range result.Entries {
		views = append(views, entryView{
			Name:     e.Name,
			Protocol: e.Inbound.Protocol,
			Panel:    e.Inbound.PanelID,
			Clash:    e.Clash != nil,
			Link:     e.Link,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"subUrl":  s.subBase(c) + "/sub/" + u.SubToken,
		"entries": views,
		// A nil slice marshals as null, which the UI would then dereference;
		// every list this API returns has to be an array.
		"warnings":    jsonList(result.Warnings),
		"clash":       yaml,
		"renderError": errString(renderErr),
		"userinfo":    subscription.UserInfo(&u),
	})
}

// jsonList makes a possibly-nil slice marshal as [] instead of null.
func jsonList[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func (s *Server) planByID(id uint) (*model.Plan, error) {
	if id == 0 {
		return nil, nil
	}
	var plan model.Plan
	if err := s.db.First(&plan, id).Error; err != nil {
		return nil, errNotFound
	}
	return &plan, nil
}

// validateUsername keeps the name usable as a 3x-ui client email; the panel
// rejects slashes, spaces and control characters.
func validateUsername(name string) error {
	if len(name) < 2 || len(name) > 40 {
		return errBadUsername
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return errBadUsername
		}
	}
	return nil
}

// trafficResetOf validates a plan's reset policy, with empty meaning def.
func trafficResetOf(c *gin.Context, mode, def string) (string, bool) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = def
	}
	if mode == "" {
		mode = model.TrafficResetNone
	}
	if !model.ValidTrafficReset(mode) {
		failMsg(c, http.StatusBadRequest, "trafficReset must be none, monthly or calendar")
		return "", false
	}
	return mode, true
}

func resetModeOf(plan *model.Plan) string {
	if plan == nil || plan.TrafficReset == "" {
		return model.TrafficResetNone
	}
	return plan.TrafficReset
}

func valueOr[T any](ptr *T, def T) T {
	if ptr == nil {
		return def
	}
	return *ptr
}
