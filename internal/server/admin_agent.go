package server

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
)

// An agent session hands the configuration to an outside agent for one task.
// The admin gets a short prompt carrying a single-use token; the agent reads a
// snapshot, edits it and submits it; the admin reviews the diff and merges it
// or returns it with feedback. Merging, cancelling or expiry ends the session,
// and the token stops working at once. Nothing the agent sends takes effect
// before a merge, and its token reaches nothing outside /api/agent.

const (
	agentTokenPrefix = "fxa_"
	ctxAgentKey      = "firex_agent"
	// agentSeenEvery throttles the activity stamp the waiting page shows.
	agentSeenEvery = 5 * time.Second
	maxAgentText   = 4000
)

//go:embed agent_guide.md
var agentGuide string

func hashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func liveAgentStates() []string {
	return []string{model.AgentOpen, model.AgentSubmitted, model.AgentReturned}
}

// ------------------------------------------------------------------ admin

type agentSessionView struct {
	model.AgentSession
	Live    bool `json:"live"`
	Expired bool `json:"expired"`
	// Stale means the configuration moved since the submission was made, so
	// merging it would return it to the agent instead.
	Stale   bool             `json:"stale"`
	Preview *snapshotPreview `json:"preview"`
	Result  *snapshotOutcome `json:"result"`
}

func (s *Server) agentView(sess model.AgentSession) agentSessionView {
	now := nowMs()
	view := agentSessionView{AgentSession: sess, Live: sess.Live(now)}
	view.Expired = !view.Live && sess.State != model.AgentMerged && sess.State != model.AgentCancelled
	if sess.Preview != "" {
		view.Preview = &snapshotPreview{}
		_ = json.Unmarshal([]byte(sess.Preview), view.Preview)
	}
	if sess.Result != "" {
		view.Result = &snapshotOutcome{}
		_ = json.Unmarshal([]byte(sess.Result), view.Result)
	}
	if view.Live && sess.State == model.AgentSubmitted {
		if doc, err := exportSnapshot(s.db); err == nil {
			view.Stale = snapshotRevision(doc) != sess.Revision
		}
	}
	return view
}

type agentSessionRequest struct {
	Task string `json:"task"`
	// TTLMinutes is how long the token works if nobody merges or cancels.
	TTLMinutes int `json:"ttlMinutes"`
}

func (s *Server) createAgentSession(c *gin.Context) {
	var req agentSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	task := strings.TrimSpace(req.Task)
	if len(task) > maxAgentText {
		failMsg(c, http.StatusBadRequest, "任务描述太长")
		return
	}
	if req.TTLMinutes < 10 || req.TTLMinutes > 24*60 {
		failMsg(c, http.StatusBadRequest, "有效期必须在 10 分钟到 24 小时之间")
		return
	}
	s.agentMu.Lock()
	defer s.agentMu.Unlock()

	now := time.Now()
	var live int64
	s.db.Model(&model.AgentSession{}).Where("state IN ? AND expires_at > ?", liveAgentStates(), now.UnixMilli()).Count(&live)
	if live > 0 {
		failMsg(c, http.StatusConflict, "已有进行中的 agent 会话，先完成或取消它")
		return
	}
	token := agentTokenPrefix + randomToken(32)
	sess := model.AgentSession{
		AdminID:   currentAdmin(c).ID,
		TokenHash: hashAgentToken(token),
		Task:      task,
		State:     model.AgentOpen,
		ExpiresAt: now.Add(time.Duration(req.TTLMinutes) * time.Minute).UnixMilli(),
		CreatedAt: now.UnixMilli(),
	}
	if err := s.db.Create(&sess).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"session": s.agentView(sess),
		"token":   token,
		"prompt":  agentPrompt(s.subBase(c), token, sess),
	})
}

// agentPrompt is the text the admin pastes into an agent: just enough
// background to start, with the guide endpoint carrying the details.
func agentPrompt(base, token string, sess model.AgentSession) string {
	api := strings.TrimRight(base, "/") + "/api/agent"
	task := sess.Task
	if task == "" {
		task = "（管理员没有填写，请询问需要做什么）"
	}
	expires := time.UnixMilli(sess.ExpiresAt).Format("2006-01-02 15:04 MST")
	return fmt.Sprintf(`你将通过 FireX Agent API 修改一套生产环境的配置。FireX 是 3x-ui 面板集群的控制面：入站编入节点组，分流方案决定每档用户能用哪些节点组，分流策略 × 方案的矩阵决定流量怎么分。你的修改不会直接生效，要等管理员审核差异后合并。

任务：
%s

API：%s
请求头：Authorization: Bearer %s
令牌只用于本次会话，%s 过期，合并后立即失效。

步骤：
1. GET %s/guide —— 先读完快照格式和规则。
2. GET %s/snapshot —— 读取当前配置，记下 revision。
3. 修改后 POST %s/preview 自检差异，可以反复调用。
4. 确认无误后 PUT %s/submission 提交，附上 summary 说明改了什么、为什么。
5. 轮询 GET %s 查看状态：returned 表示被退回，按 feedback 重新读取并修改后再提交；merged 表示完成。`,
		task, api, token, expires, api, api, api, api, api)
}

func (s *Server) currentAgentSession(c *gin.Context) {
	var sess model.AgentSession
	err := s.db.Where("state IN ? AND expires_at > ?", liveAgentStates(), nowMs()).Order("id DESC").First(&sess).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"session": nil})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"session": s.agentView(sess)})
}

// agentHistoryItem is one row of the history list; the full review, diff
// included, is a GET of the session itself.
type agentHistoryItem struct {
	ID          uint   `json:"id"`
	Task        string `json:"task"`
	State       string `json:"state"`
	Live        bool   `json:"live"`
	Expired     bool   `json:"expired"`
	Summary     string `json:"summary"`
	Submissions int    `json:"submissions"`
	Changes     int    `json:"changes"`
	Admin       string `json:"admin"`
	CreatedAt   int64  `json:"createdAt"`
	FinishedAt  int64  `json:"finishedAt"`
}

func (s *Server) listAgentSessions(c *gin.Context) {
	var sessions []model.AgentSession
	if err := s.db.Order("id DESC").Limit(100).Find(&sessions).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	var admins []model.Admin
	s.db.Find(&admins)
	names := map[uint]string{}
	for _, admin := range admins {
		names[admin.ID] = admin.Username
	}
	now := nowMs()
	items := make([]agentHistoryItem, 0, len(sessions))
	for _, sess := range sessions {
		item := agentHistoryItem{
			ID: sess.ID, Task: sess.Task, State: sess.State, Live: sess.Live(now),
			Summary: sess.Summary, Submissions: sess.Submissions, Admin: names[sess.AdminID],
			CreatedAt: sess.CreatedAt, FinishedAt: sess.FinishedAt,
		}
		item.Expired = !item.Live && sess.State != model.AgentMerged && sess.State != model.AgentCancelled
		if sess.Preview != "" {
			var preview snapshotPreview
			if json.Unmarshal([]byte(sess.Preview), &preview) == nil {
				item.Changes = len(preview.Changes)
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) loadAgentSession(c *gin.Context) (model.AgentSession, bool) {
	var sess model.AgentSession
	id, ok := paramID(c)
	if !ok {
		return sess, false
	}
	if err := s.db.First(&sess, id).Error; err != nil {
		failMsg(c, http.StatusNotFound, "会话不存在")
		return sess, false
	}
	return sess, true
}

func (s *Server) getAgentSession(c *gin.Context) {
	if sess, ok := s.loadAgentSession(c); ok {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"session": s.agentView(sess)})
	}
}

type agentMergeRequest struct {
	// Submissions is the count the reviewer saw, so a submission that arrived
	// while they were reading is never merged unseen.
	Submissions int `json:"submissions"`
	// AllowPanelRemoval confirms a submission that deletes panels.
	AllowPanelRemoval bool `json:"allowPanelRemoval"`
}

// staleFeedback is what an agent reads when its submission was overtaken.
const staleFeedback = "生产配置在你提交之后被修改过。请重新读取快照，基于最新配置重做修改后再提交。"

func (s *Server) mergeAgentSession(c *gin.Context) {
	var req agentMergeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	sess, ok := s.loadAgentSession(c)
	if !ok {
		return
	}
	switch {
	case !sess.Live(nowMs()):
		failMsg(c, http.StatusConflict, "会话已结束或已过期")
		return
	case sess.State != model.AgentSubmitted:
		failMsg(c, http.StatusConflict, "agent 还没有提交，或提交已被退回")
		return
	case req.Submissions != sess.Submissions:
		failMsg(c, http.StatusConflict, "agent 又提交了新版本，请重新审核")
		return
	}
	var doc snapshotRequest
	if err := json.Unmarshal([]byte(sess.Submission), &doc); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	result, err := s.runSnapshot(&doc, false, req.AllowPanelRemoval)
	if errors.Is(err, errSnapshotStale) {
		// The agent rebuilds on the current configuration; merging it as it
		// stands would revert whatever changed meanwhile.
		sess.State, sess.Feedback = model.AgentReturned, staleFeedback
		s.db.Model(&sess).Select("state", "feedback").Updates(&sess)
		c.JSON(http.StatusConflict, gin.H{
			"error":   "生产配置在 agent 提交后被修改过，已退回 agent 基于最新配置重做",
			"session": s.agentView(sess),
		})
		return
	}
	if err != nil {
		failSnapshot(c, err)
		return
	}
	outcome := s.settleSnapshot(result)
	encoded, _ := json.Marshal(outcome)
	sess.State, sess.Result, sess.FinishedAt = model.AgentMerged, string(encoded), nowMs()
	if err := s.db.Model(&sess).Select("state", "result", "finished_at").Updates(&sess).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"session": s.agentView(sess)})
}

func (s *Server) returnAgentSession(c *gin.Context) {
	var req struct {
		Feedback string `json:"feedback"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	feedback := strings.TrimSpace(req.Feedback)
	if feedback == "" || len(feedback) > maxAgentText {
		failMsg(c, http.StatusBadRequest, "请写下退回意见，agent 会按它修改")
		return
	}
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	sess, ok := s.loadAgentSession(c)
	if !ok {
		return
	}
	if !sess.Live(nowMs()) || sess.State != model.AgentSubmitted {
		failMsg(c, http.StatusConflict, "只有待审核的提交可以退回")
		return
	}
	sess.State, sess.Feedback = model.AgentReturned, feedback
	if err := s.db.Model(&sess).Select("state", "feedback").Updates(&sess).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s.agentView(sess)})
}

func (s *Server) cancelAgentSession(c *gin.Context) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	sess, ok := s.loadAgentSession(c)
	if !ok {
		return
	}
	if !sess.Live(nowMs()) {
		failMsg(c, http.StatusConflict, "会话已结束或已过期")
		return
	}
	sess.State, sess.FinishedAt = model.AgentCancelled, nowMs()
	if err := s.db.Model(&sess).Select("state", "finished_at").Updates(&sess).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s.agentView(sess)})
}

// ------------------------------------------------------------------ agent

func (s *Server) requireAgent(c *gin.Context) {
	scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || !strings.HasPrefix(token, agentTokenPrefix) {
		failMsg(c, http.StatusUnauthorized, "需要 Authorization: Bearer <会话令牌>")
		return
	}
	var sess model.AgentSession
	if err := s.db.First(&sess, "token_hash = ?", hashAgentToken(token)).Error; err != nil {
		failMsg(c, http.StatusUnauthorized, "无效的会话令牌")
		return
	}
	now := nowMs()
	if !sess.Live(now) {
		msg := "会话已过期，令牌失效"
		switch sess.State {
		case model.AgentMerged:
			msg = "修改已合并，会话结束，令牌失效"
		case model.AgentCancelled:
			msg = "管理员已取消会话，令牌失效"
		}
		failMsg(c, http.StatusUnauthorized, msg)
		return
	}
	if now-sess.LastSeenAt >= agentSeenEvery.Milliseconds() {
		s.db.Model(&model.AgentSession{}).Where("id = ?", sess.ID).Update("last_seen_at", now)
	}
	c.Set(ctxAgentKey, sess.ID)
	c.Next()
}

func currentAgentID(c *gin.Context) uint {
	id, _ := c.Get(ctxAgentKey)
	value, _ := id.(uint)
	return value
}

func (s *Server) agentSession(c *gin.Context) {
	var sess model.AgentSession
	if err := s.db.First(&sess, currentAgentID(c)).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"state": sess.State, "task": sess.Task, "feedback": sess.Feedback,
		"summary": sess.Summary, "submissions": sess.Submissions,
		"submittedAt": sess.SubmittedAt, "expiresAt": sess.ExpiresAt,
	})
}

func (s *Server) agentGuide(c *gin.Context) {
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(agentGuide))
}

func (s *Server) agentSnapshot(c *gin.Context) {
	doc, err := exportSnapshot(s.db)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	s.db.Model(&model.AgentSession{}).Where("id = ? AND read_at = 0", currentAgentID(c)).Update("read_at", nowMs())
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, struct {
		Revision string `json:"revision"`
		snapshotDoc
	}{snapshotRevision(doc), doc})
}

func (s *Server) agentPreview(c *gin.Context) {
	var req snapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	result, err := s.runSnapshot(&req, true, true)
	if err != nil {
		failSnapshot(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result.preview())
}

type agentSubmission struct {
	snapshotRequest
	Summary string `json:"summary"`
}

func (s *Server) agentSubmit(c *gin.Context) {
	var req agentSubmission
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" || len(summary) > maxAgentText {
		failMsg(c, http.StatusBadRequest, "请在 summary 里说明改了什么、为什么")
		return
	}
	// A submission is validated as a merge would be, so the reviewer only
	// ever sees documents that can actually be applied.
	result, err := s.runSnapshot(&req.snapshotRequest, true, true)
	if err != nil {
		failSnapshot(c, err)
		return
	}
	preview := result.preview()
	if len(preview.Changes) == 0 {
		failMsg(c, http.StatusBadRequest, "提交里没有任何改动")
		return
	}

	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	var sess model.AgentSession
	if err := s.db.First(&sess, currentAgentID(c)).Error; err != nil || !sess.Live(nowMs()) {
		failMsg(c, http.StatusUnauthorized, "会话已结束，令牌失效")
		return
	}
	submission, _ := json.Marshal(req.snapshotRequest)
	encoded, _ := json.Marshal(preview)
	err = s.db.Model(&model.AgentSession{}).Where("id = ?", sess.ID).Updates(map[string]any{
		"state": model.AgentSubmitted, "submission": string(submission), "summary": summary,
		"preview": string(encoded), "revision": req.Revision,
		"submissions": sess.Submissions + 1, "submitted_at": nowMs(),
	}).Error
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"state": model.AgentSubmitted, "submissions": sess.Submissions + 1, "preview": preview})
}
