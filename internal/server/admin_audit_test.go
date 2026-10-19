package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAuditLogRecordsWritesByName(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	h.do(http.MethodPost, "/api/panels/test", map[string]any{"baseUrl": h.fake.URL(), "apiToken": "tok"})
	h.do(http.MethodPost, "/api/plans", map[string]any{"name": ""}) // rejected
	h.mustDo(http.MethodDelete, "/api/users/"+itoa(u.ID), nil)

	var out struct {
		Items []struct {
			Actor, Action, Target string
		} `json:"items"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/audit-logs", nil), &out)
	if len(out.Items) == 0 {
		t.Fatal("audit log is empty")
	}
	latest := out.Items[0]
	if latest.Action != "删除用户" || latest.Target != "carol" || latest.Actor != "admin" {
		t.Errorf("latest entry = %+v, want admin 删除用户 carol", latest)
	}
	var actions []string
	for _, item := range out.Items {
		actions = append(actions, item.Action+":"+item.Target)
	}
	joined := strings.Join(actions, ",")
	for _, want := range []string{"新增面板:p1", "修改入站:hk", "新增套餐:标准", "新增用户:carol"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit log %s is missing %s", joined, want)
		}
	}
	if len(out.Items) != 9 {
		t.Errorf("audit log %s has %d entries, want 9: the panel test and the rejected plan leave none", joined, len(out.Items))
	}
}

func TestAgentHistoryListsFinishedSessions(t *testing.T) {
	h := newHarness(t)
	h.login()
	first := h.startAgent()
	h.mustDo(http.MethodPost, "/api/agent-sessions/"+itoa(first.Session.ID)+"/cancel", nil)
	h.startAgent()

	var out struct {
		Items []agentHistoryItem `json:"items"`
	}
	json.Unmarshal(h.mustDo(http.MethodGet, "/api/agent-sessions", nil), &out)
	if len(out.Items) != 2 || !out.Items[0].Live || out.Items[1].State != "cancelled" || out.Items[1].Admin != "admin" {
		t.Errorf("history = %+v, want the live session then the cancelled one", out.Items)
	}
}
