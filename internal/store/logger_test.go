package store

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/model"
)

func TestSetDebugTogglesStatementTracing(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "firex.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var out bytes.Buffer
	db.Config.Logger.(*sqlLogger).out = log.New(&out, "", 0)

	out.Reset()
	var count int64
	db.Model(&model.Setting{}).Count(&count)
	if out.Len() != 0 {
		t.Fatalf("quiet database traced a statement:\n%s", out.String())
	}

	db.SetDebug(true)
	db.Model(&model.Setting{}).Count(&count)
	line := out.String()
	if !strings.Contains(line, "SELECT count(*)") || !strings.Contains(line, "logger_test.go:") {
		t.Fatalf("debug trace missing statement or its caller:\n%s", line)
	}

	db.SetDebug(false)
	out.Reset()
	db.Model(&model.Setting{}).Count(&count)
	if out.Len() != 0 {
		t.Fatalf("tracing stayed on after SetDebug(false):\n%s", out.String())
	}
}
