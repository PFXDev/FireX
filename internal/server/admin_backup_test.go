package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/backup"
	"github.com/PFXDev/FireX/internal/model"
)

func (h *harness) backups() backupListResponse {
	h.t.Helper()
	var list backupListResponse
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/backups", nil), &list); err != nil {
		h.t.Fatal(err)
	}
	return list
}

type backupListResponse struct {
	Items          []backup.Entry `json:"items"`
	Dir            string         `json:"dir"`
	PendingRestore bool           `json:"pendingRestore"`
}

func (h *harness) uploadBackup(name string, content []byte) (*http.Response, []byte) {
	h.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", name)
	part.Write(content)
	form.Close()
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/backups/upload", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	raw.ReadFrom(resp.Body)
	return resp, raw.Bytes()
}

func TestBackupCreateDownloadRestoreDelete(t *testing.T) {
	h := newHarness(t)
	h.login()

	var created backup.Entry
	if err := json.Unmarshal(h.mustDo(http.MethodPost, "/api/backups", nil), &created); err != nil {
		t.Fatal(err)
	}
	if created.Kind != backup.KindManual || created.Size == 0 {
		t.Fatalf("created = %+v", created)
	}

	resp, raw := h.do(http.MethodGet, "/api/backups/"+created.Name+"/download", nil)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Fatalf("download = %d, %d bytes", resp.StatusCode, len(raw))
	}

	// A downloaded backup goes back in as an upload.
	resp, body := h.uploadBackup("from-elsewhere.db", raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d: %s", resp.StatusCode, body)
	}
	var uploaded backup.Entry
	json.Unmarshal(body, &uploaded)
	if uploaded.Kind != backup.KindUpload {
		t.Fatalf("uploaded = %+v", uploaded)
	}

	var restored struct {
		PreRestore string `json:"preRestore"`
	}
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/backups/"+uploaded.Name+"/restore", nil), &restored)
	if !strings.HasPrefix(restored.PreRestore, "pre-restore-") {
		t.Fatalf("preRestore = %q", restored.PreRestore)
	}
	list := h.backups()
	if !list.PendingRestore {
		t.Fatal("restore not reported as pending")
	}
	if len(list.Items) != 3 {
		t.Fatalf("got %d backups, want manual, upload and pre-restore", len(list.Items))
	}
	// A second restore must wait for the first to be applied.
	if resp, _ := h.do(http.MethodPost, "/api/backups/"+created.Name+"/restore", nil); resp.StatusCode == http.StatusOK {
		t.Fatal("second restore staged on top of a pending one")
	}

	h.mustDo(http.MethodDelete, "/api/backups/"+created.Name, nil)
	if resp, _ := h.do(http.MethodDelete, "/api/backups/"+created.Name, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleting twice = %d, want 404", resp.StatusCode)
	}

	var logs []model.AuditLog
	h.db.Order("id").Find(&logs)
	var actions []string
	for _, l := range logs {
		actions = append(actions, l.Action+":"+l.Target)
	}
	joined := strings.Join(actions, " ")
	for _, want := range []string{"创建数据库备份:" + created.Name, "还原数据库备份:" + uploaded.Name, "删除数据库备份:" + created.Name} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit log %v lacks %q", actions, want)
		}
	}
}

func TestBackupRestoreRollsBackWhenRestartFails(t *testing.T) {
	h := newHarness(t)
	h.login()
	var created backup.Entry
	json.Unmarshal(h.mustDo(http.MethodPost, "/api/backups", nil), &created)

	h.srv.restart = func(string) error { return os.ErrPermission }
	if resp, body := h.do(http.MethodPost, "/api/backups/"+created.Name+"/restore", nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("restore = %d: %s", resp.StatusCode, body)
	}
	if h.backups().PendingRestore {
		t.Fatal("restore left staged after the restart was refused")
	}
}

func TestBackupRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	h.login()

	if resp, body := h.uploadBackup("notes.db", []byte("not a database")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload junk = %d: %s", resp.StatusCode, body)
	}
	for _, name := range []string{"..%2Ffirex.db", "firex.db", "manual-20260101-000000.db"} {
		for _, op := range []struct{ method, path string }{
			{http.MethodGet, "/api/backups/" + name + "/download"},
			{http.MethodPost, "/api/backups/" + name + "/restore"},
			{http.MethodDelete, "/api/backups/" + name},
		} {
			if resp, _ := h.do(op.method, op.path, nil); resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", op.method, op.path, resp.StatusCode)
			}
		}
	}
	if h.backups().PendingRestore {
		t.Fatal("bad input staged a restore")
	}
}

func TestBackupsAreSuperAdminOnly(t *testing.T) {
	h := newHarness(t)
	makeOperator(t, h, "ops")
	h.loginAs("ops", "password123")
	for _, op := range []struct{ method, path string }{
		{http.MethodGet, "/api/backups"},
		{http.MethodPost, "/api/backups"},
		{http.MethodPost, "/api/backups/manual-20260101-000000.db/restore"},
	} {
		if resp, _ := h.do(op.method, op.path, nil); resp.StatusCode != http.StatusForbidden {
			t.Errorf("operator %s %s = %d, want 403", op.method, op.path, resp.StatusCode)
		}
	}
}
