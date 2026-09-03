package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
}

type planRequest struct {
	Name         string `json:"name"`
	ProfileID    *uint  `json:"profileId"`
	TrafficBytes int64  `json:"trafficBytes"`
	DurationDays int    `json:"durationDays"`
	DeviceLimit  int    `json:"deviceLimit"`
	SpeedNote    string `json:"speedNote"`
	Enabled      *bool  `json:"enabled"`
	SortOrder    *int   `json:"sortOrder"`
	Remark       string `json:"remark"`
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
	out := make([]planRow, 0, len(plans))
	for _, p := range plans {
		row := planRow{Plan: p, ProfileName: profileNames[p.ProfileID]}
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
	now := provision.NowMs()
	plan := model.Plan{
		Name:         strings.TrimSpace(req.Name),
		ProfileID:    valueOr(req.ProfileID, 0),
		TrafficBytes: req.TrafficBytes,
		DurationDays: req.DurationDays,
		DeviceLimit:  req.DeviceLimit,
		SpeedNote:    req.SpeedNote,
		Enabled:      req.Enabled == nil || *req.Enabled,
		SortOrder:    valueOr(req.SortOrder, 100),
		Remark:       req.Remark,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&plan).Error; err != nil {
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
	if err := s.db.Save(&plan).Error; err != nil {
		fail(c, http.StatusBadRequest, err)
		return
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
	if err := s.db.Delete(&model.Plan{}, id).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// -------------------------------------------------------------------- users

type userRow struct {
	model.User
	PlanName     string   `json:"planName"`
	Used         int64    `json:"used"`
	InboundCount int      `json:"inboundCount"`
	SyncState    string   `json:"syncState"`
	SyncError    []string `json:"syncErrors"`
	SubURL       string   `json:"subUrl"`
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
	planNames := map[uint]string{}
	var plans []model.Plan
	s.db.Find(&plans)
	for _, p := range plans {
		planNames[p.ID] = p.Name
	}

	base := s.subBase(c)
	out := make([]userRow, 0, len(users))
	for i := range users {
		u := users[i]
		row := userRow{
			User:      u,
			PlanName:  planNames[u.PlanID],
			Used:      u.Upload + u.Download,
			SyncState: model.SyncStateSynced,
			SyncError: []string{},
			SubURL:    base + "/sub/" + u.SubToken,
		}
		inbounds, _ := s.mgr.InboundsForUser(&u)
		row.InboundCount = len(inbounds)

		var records []model.UserPanel
		s.db.Where("user_id = ?", u.ID).Find(&records)
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
	var records []model.UserPanel
	s.db.Where("user_id = ?", u.ID).Find(&records)
	remoteFailures := 0
	for _, rec := range records {
		var p model.Panel
		if err := s.db.First(&p, rec.PanelID).Error; err != nil {
			continue
		}
		if err := s.mgr.ClientFor(&p).ResetClientTraffic(ctx, rec.Email); err != nil {
			remoteFailures++
		}
	}
	// Zero the baselines too, otherwise the next poll reads the panel's fresh
	// counter as a delta against the old high-water mark.
	s.db.Model(&model.UserPanel{}).Where("user_id = ?", u.ID).
		Updates(map[string]any{"last_up": 0, "last_down": 0})

	u.Upload, u.Download, u.Depleted = 0, 0, false
	u.UpdatedAt = provision.NowMs()
	if err := s.db.Save(&u).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	syncErr := s.mgr.ReconcileUser(ctx, &u)
	c.JSON(http.StatusOK, gin.H{"ok": true, "remoteFailures": remoteFailures, "syncError": errString(syncErr)})
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
	yaml, renderErr := s.subs.Clash(result)

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

func valueOr[T any](ptr *T, def T) T {
	if ptr == nil {
		return def
	}
	return *ptr
}
