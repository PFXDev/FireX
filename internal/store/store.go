// Package store owns the FireX database handle and schema migration.
package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
)

type DB struct{ *gorm.DB }

func Open(path string, debug bool) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	sqlLog := &sqlLogger{out: log.Default()}
	sqlLog.verbose.Store(debug)
	// _pragma busy_timeout keeps concurrent sync jobs from failing on SQLITE_BUSY.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: sqlLog,
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db := &DB{gdb}
	if err := db.migrate(path); err != nil {
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (d *DB) GetSetting(key, def string) string {
	var s model.Setting
	if err := d.First(&s, "key = ?", key).Error; err != nil {
		return def
	}
	return s.Value
}

func (d *DB) SetSetting(key, value string) error {
	return d.Save(&model.Setting{Key: key, Value: value}).Error
}

// IsNotFound reports a missing-row error without leaking gorm into callers.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
