package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
)

// The audit log records every configuration change made through the admin
// API: one row per successful write, naming the admin, the action and the
// object. Reads, dry runs and failed requests leave nothing behind.

const (
	auditKeep     = 10000
	auditPageSize = 50
)

// auditActions labels each write route for the log page. A route missing here
// is still logged, under its method and path.
var auditActions = map[string]string{
	"POST /api/auth/password":                     "修改管理员密码",
	"POST /api/admins":                            "新增管理员",
	"PUT /api/admins/:id":                         "修改管理员",
	"DELETE /api/admins/:id":                      "删除管理员",
	"POST /api/admins/:id/resetPassword":          "重置管理员密码",
	"POST /api/sync":                              "立即同步",
	"POST /api/agent-sessions":                    "生成 agent 会话",
	"POST /api/agent-sessions/:id/merge":          "合并 agent 修改",
	"POST /api/agent-sessions/:id/return":         "退回 agent 修改",
	"POST /api/agent-sessions/:id/cancel":         "取消 agent 会话",
	"POST /api/panels":                            "新增面板",
	"PUT /api/panels/:id":                         "修改面板",
	"DELETE /api/panels/:id":                      "删除面板",
	"POST /api/panels/:id/discover":               "发现面板入站",
	"POST /api/panels/:id/resetTraffic":           "重置面板流量",
	"PUT /api/inbounds/:id":                       "修改入站",
	"POST /api/inbounds/bulk":                     "批量修改入站",
	"DELETE /api/inbounds/:id":                    "删除入站",
	"POST /api/inbounds/:id/config":               "接管入站配置",
	"PUT /api/inbounds/:id/config":                "修改入站配置",
	"DELETE /api/inbounds/:id/config":             "释放入站配置",
	"POST /api/inbounds/:id/config/deploy":        "下发入站配置",
	"POST /api/inbound-templates":                 "新增入站模板",
	"PUT /api/inbound-templates/:id":              "修改入站模板",
	"DELETE /api/inbound-templates/:id":           "删除入站模板",
	"POST /api/inbound-templates/:id/instantiate": "用模板创建入站",
	"POST /api/node-groups":                       "新增节点组",
	"PUT /api/node-groups/:id":                    "修改节点组",
	"DELETE /api/node-groups/:id":                 "删除节点组",
	"POST /api/profiles":                          "新增分流方案",
	"PUT /api/profiles/:id":                       "修改分流方案",
	"DELETE /api/profiles/:id":                    "删除分流方案",
	"PUT /api/routing":                            "修改分流矩阵",
	"POST /api/routing/presets/:key":              "应用分流预设",
	"POST /api/plans":                             "新增套餐",
	"PUT /api/plans/:id":                          "修改套餐",
	"DELETE /api/plans/:id":                       "删除套餐",
	"POST /api/users":                             "新增用户",
	"PUT /api/users/:id":                          "修改用户",
	"DELETE /api/users/:id":                       "删除用户",
	"POST /api/users/:id/resync":                  "重新下发用户",
	"POST /api/users/:id/resetTraffic":            "重置用户流量",
	"PUT /api/settings/clashTemplate":             "修改 Clash 模板",
	"PUT /api/settings/server":                    "修改服务设置",
	"POST /api/update/apply":                      "执行更新",
}

// auditSkip lists writes that change nothing: tests, previews and checks.
var auditSkip = map[string]bool{
	"POST /api/panels/test":                 true,
	"POST /api/inbounds/:id/config/preview": true,
	"POST /api/update/check":                true,
	"POST /api/update/dismiss":              true,
}

// auditNames resolves the :id of a route to the object's name, read before the
// handler runs so a delete still has one.
var auditNames = map[string]struct{ table, column string }{
	"panels":            {"panels", "name"},
	"inbounds":          {"inbounds", "COALESCE(NULLIF(name, ''), remote_remark)"},
	"inbound-templates": {"inbound_templates", "name"},
	"node-groups":       {"node_groups", "name"},
	"profiles":          {"profiles", "name"},
	"plans":             {"plans", "name"},
	"users":             {"users", "username"},
	"agent-sessions":    {"agent_sessions", "task"},
	"admins":            {"admins", "username"},
}

func (s *Server) audit(c *gin.Context) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		c.Next()
		return
	}
	route := c.Request.Method + " " + c.FullPath()
	if auditSkip[route] {
		c.Next()
		return
	}
	target := s.auditTarget(c)
	if runes := []rune(target); len(runes) > 60 {
		target = string(runes[:60]) + "…"
	}
	c.Next()
	if c.Writer.Status() >= http.StatusBadRequest {
		return
	}
	action := auditActions[route]
	if action == "" {
		action = route
	}
	entry := model.AuditLog{
		Action:    action,
		Target:    target,
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		IP:        c.ClientIP(),
		CreatedAt: nowMs(),
	}
	if admin := currentAdmin(c); admin != nil {
		entry.Actor = admin.Username
	}
	if s.db.Create(&entry).Error == nil && entry.ID%100 == 0 && entry.ID > auditKeep {
		s.db.Where("id <= ?", entry.ID-auditKeep).Delete(&model.AuditLog{})
	}
}

// auditTarget names the object a request acts on: the record behind :id, the
// preset behind :key, or the name (or agent task) a create carries in its body.
func (s *Server) auditTarget(c *gin.Context) string {
	if key := c.Param("key"); key != "" {
		return key
	}
	if id := c.Param("id"); id != "" {
		segment := strings.Split(strings.TrimPrefix(c.FullPath(), "/api/"), "/")[0]
		if ref, ok := auditNames[segment]; ok {
			var name string
			s.db.Table(ref.table).Select(ref.column).Where("id = ?", id).Scan(&name)
			if name != "" {
				return name
			}
		}
		return "#" + id
	}
	if c.Request.Method != http.MethodPost || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var named struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Task     string `json:"task"`
	}
	_ = json.Unmarshal(body, &named)
	for _, name := range []string{named.Name, named.Username, named.Task} {
		if name != "" {
			return name
		}
	}
	return ""
}

func (s *Server) listAuditLogs(c *gin.Context) {
	query := s.db.Model(&model.AuditLog{}).Order("id DESC").Limit(auditPageSize + 1)
	if before, err := strconv.ParseUint(c.Query("before"), 10, 64); err == nil && before > 0 {
		query = query.Where("id < ?", before)
	}
	var items []model.AuditLog
	if err := query.Find(&items).Error; err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	more := len(items) > auditPageSize
	if more {
		items = items[:auditPageSize]
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "more": more})
}
