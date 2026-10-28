package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/store"
)

func openStore(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return db
}

func TestStagedRestoreReplacesDatabaseOnNextOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "firex.db")
	db := openStore(t, dbPath)
	if err := db.Create(&model.Admin{Username: "old", PasswordHash: "x", Role: model.AdminRoleSuperAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "manual.db")
	if err := db.Snapshot(backupPath); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if info, err := os.Stat(backupPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}

	// Changes made after the backup must be gone once it is restored.
	db.Create(&model.Admin{Username: "later", PasswordHash: "x", Role: model.AdminRoleSuperAdmin})
	db.Create(&model.Session{Token: "t", AdminID: 1, ExpiresAt: 1 << 62})

	if err := store.StageRestore(dbPath, backupPath, store.RestoreNote{Backup: "manual.db", Actor: "old", IP: "127.0.0.1"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !store.PendingRestore(dbPath) {
		t.Fatal("restore not reported as pending")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openStore(t, dbPath)
	defer db.Close()
	if store.PendingRestore(dbPath) {
		t.Fatal("restore still pending after open")
	}
	var names []string
	db.Model(&model.Admin{}).Pluck("username", &names)
	if strings.Join(names, ",") != "old" {
		t.Fatalf("admins = %v, want [old]", names)
	}
	var sessions int64
	db.Model(&model.Session{}).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0", sessions)
	}
	var audit model.AuditLog
	if err := db.Order("id desc").First(&audit).Error; err != nil || audit.Target != "manual.db" || audit.Actor != "old" {
		t.Fatalf("audit = %+v, %v; want restore of manual.db by old", audit, err)
	}
}

func TestCancelRestoreKeepsDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "firex.db")
	db := openStore(t, dbPath)
	db.Create(&model.Admin{Username: "a", PasswordHash: "x", Role: model.AdminRoleSuperAdmin})
	backupPath := filepath.Join(dir, "b.db")
	if err := db.Snapshot(backupPath); err != nil {
		t.Fatal(err)
	}
	if err := store.StageRestore(dbPath, backupPath, store.RestoreNote{}); err != nil {
		t.Fatal(err)
	}
	store.CancelRestore(dbPath)
	if store.PendingRestore(dbPath) {
		t.Fatal("restore still pending after cancel")
	}
	for _, suffix := range []string{".restore", ".restore.json"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", suffix, err)
		}
	}
	db.Close()
}

func TestCheckBackupRejectsUnusableFiles(t *testing.T) {
	dir := t.TempDir()

	noAdmin := filepath.Join(dir, "empty.db")
	db := openStore(t, filepath.Join(dir, "src.db"))
	if err := db.Snapshot(noAdmin); err != nil {
		t.Fatal(err)
	}
	db.Close()

	notSQLite := filepath.Join(dir, "text.db")
	os.WriteFile(notSQLite, []byte("hello, this is not a database"), 0o600)

	truncated := filepath.Join(dir, "short.db")
	os.WriteFile(truncated, []byte("SQLite"), 0o600)

	for name, path := range map[string]string{
		"no admin":   noAdmin,
		"not sqlite": notSQLite,
		"truncated":  truncated,
		"missing":    filepath.Join(dir, "nope.db"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.CheckBackup(path); err == nil {
				t.Fatal("CheckBackup accepted it")
			}
		})
	}
}
