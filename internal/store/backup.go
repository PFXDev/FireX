package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/PFXDev/FireX/internal/model"
)

// A restore cannot swap the database under a running process: every component
// holds the open handle. It is staged beside the database instead and put in
// place by the next Open, before anything has the file open.
const (
	restoreSuffix     = ".restore"
	restoreNoteSuffix = ".restore.json"
)

// RestoreNote names who asked for a staged restore, so the restored database's
// audit log can record it; the request itself was logged to the database it
// replaced.
type RestoreNote struct {
	Backup string `json:"backup"`
	Actor  string `json:"actor"`
	IP     string `json:"ip"`
}

// Snapshot writes a consistent copy of the open database to target, readable
// only by the owner since it holds panel tokens and password hashes. VACUUM
// INTO folds the WAL in, which a plain file copy would miss. It writes under a
// temporary name first, so target never names a half-written copy.
func (d *DB) Snapshot(target string) error {
	tmp := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".tmp")
	_ = os.Remove(tmp)
	if err := d.Exec("VACUUM INTO ?", tmp).Error; err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

var sqliteMagic = []byte("SQLite format 3\x00")

// CheckBackup refuses a file the next start could not run on: not SQLite,
// damaged, from a newer schema than this build knows, or without an admin to
// sign in as. It only reads the file.
func CheckBackup(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	header := make([]byte, len(sqliteMagic))
	_, err = io.ReadFull(f, header)
	f.Close()
	if err != nil || !bytes.Equal(header, sqliteMagic) {
		return errors.New("不是 SQLite 数据库文件")
	}

	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return fmt.Errorf("打开备份失败：%w", err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		defer sqlDB.Close()
	}
	db := &DB{gdb}

	var result string
	if err := db.Raw("PRAGMA quick_check").Row().Scan(&result); err != nil || result != "ok" {
		return fmt.Errorf("备份文件已损坏：%s", firstNonEmpty(result, errString(err)))
	}
	if !db.hasTable("admins") {
		return errors.New("备份中没有 FireX 数据（缺少 admins 表）")
	}
	var admins int64
	if err := db.Table("admins").Count(&admins).Error; err != nil || admins == 0 {
		return errors.New("备份中没有管理员账号，还原后将无法登录")
	}
	if db.hasTable("settings") {
		var version string
		db.Table("settings").Select("value").Where("key = ?", settingSchemaVersion).Scan(&version)
		if v, err := strconv.Atoi(version); err == nil && v > schemaVersion {
			return fmt.Errorf("备份来自更新版本的 FireX（schema v%d），当前版本只支持到 v%d", v, schemaVersion)
		}
	}
	return nil
}

// StageRestore checks src and copies it into place for the next start of the
// database at dbPath. The copy is the staged file itself, so a backup deleted
// or pruned before the restart still restores.
func StageRestore(dbPath, src string, note RestoreNote) error {
	if err := CheckBackup(src); err != nil {
		return err
	}
	raw, err := json.Marshal(note)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(dbPath+restoreNoteSuffix, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("stage restore: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := writeFileAtomic(dbPath+restoreSuffix, in); err != nil {
		_ = os.Remove(dbPath + restoreNoteSuffix)
		return fmt.Errorf("stage restore: %w", err)
	}
	return nil
}

// PendingRestore reports whether a restore is staged for the next start.
func PendingRestore(dbPath string) bool {
	_, err := os.Stat(dbPath + restoreSuffix)
	return err == nil
}

// CancelRestore drops a staged restore, for a restart that could not be started.
func CancelRestore(dbPath string) {
	_ = os.Remove(dbPath + restoreSuffix)
	_ = os.Remove(dbPath + restoreNoteSuffix)
}

// applyPendingRestore moves a staged restore over the database before it is
// opened. The old WAL and shared-memory files belong to the replaced database
// and would be replayed into the restored one, so they go first.
func applyPendingRestore(dbPath string) (*RestoreNote, error) {
	staged := dbPath + restoreSuffix
	if _, err := os.Stat(staged); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	note := &RestoreNote{}
	if raw, err := os.ReadFile(dbPath + restoreNoteSuffix); err == nil {
		_ = json.Unmarshal(raw, note)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("apply staged restore: %w", err)
		}
	}
	if err := os.Rename(staged, dbPath); err != nil {
		return nil, fmt.Errorf("apply staged restore: %w", err)
	}
	_ = os.Remove(dbPath + restoreNoteSuffix)
	return note, nil
}

// recordRestore writes the restore into the restored database's audit log and
// signs every admin out. The backup's sessions include ones that were logged
// out or belong to admins removed since, and none of them should come back.
func (d *DB) recordRestore(note *RestoreNote) {
	d.Where("1 = 1").Delete(&model.Session{})
	d.Create(&model.AuditLog{
		Actor:     note.Actor,
		Action:    "还原数据库备份",
		Target:    note.Backup,
		Method:    "POST",
		Path:      "/api/backups/" + note.Backup + "/restore",
		IP:        note.IP,
		CreatedAt: time.Now().UnixMilli(),
	})
}

// writeFileAtomic replaces path with the contents of r, owner-only, so a crash
// mid-copy never leaves a truncated file under the final name.
func writeFileAtomic(path string, r io.Reader) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".firex-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
