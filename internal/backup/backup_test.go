package backup_test

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/backup"
	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/store"
)

func service(t *testing.T, db *store.DB, dbPath, dataDir string, cfg func() config.Backup) *backup.Service {
	t.Helper()
	return backup.New(db, dbPath, dataDir, cfg, log.New(io.Discard, "", 0))
}

func TestCreateNumbersSameSecondBackups(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := service(t, db, dbPath, t.TempDir(), func() config.Backup { return config.Backup{} })
	first, err := s.Create(backup.KindManual)
	if err != nil || first.Kind != backup.KindManual {
		t.Fatalf("Create = %+v, %v", first, err)
	}
	second, err := s.Create(backup.KindManual)
	if err != nil {
		t.Fatal(err)
	}
	// Two backups in the same second get the same stamp, the second one with
	// a -2 suffix; across a second boundary they simply differ.
	if second.Name == first.Name {
		t.Fatalf("both backups named %s", first.Name)
	}
	if second.CreatedAt == first.CreatedAt && second.Name != strings.TrimSuffix(first.Name, ".db")+"-2.db" {
		t.Fatalf("second = %s; want %s with a -2 suffix", second.Name, first.Name)
	}
	if info, err := os.Stat(filepath.Join(s.Dir(), first.Name)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dataDir := t.TempDir()
	s := service(t, db, dbPath, dataDir, func() config.Backup { return config.Backup{} })
	old := seed(t, dataDir, backup.KindAuto, 48*time.Hour)
	first, _ := s.Create(backup.KindManual)
	second, _ := s.Create(backup.KindManual)

	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{second.Name, first.Name, old}
	if !slices.Equal(names, want) {
		t.Fatalf("List = %v; want %v", names, want)
	}
}

// seed writes a placeholder backup of kind stamped age ago. List and pruning
// only look at names, so the content does not matter.
func seed(t *testing.T, dataDir string, kind backup.Kind, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := string(kind) + "-" + time.Now().Add(-age).Format("20060102-150405") + ".db"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// runOnce makes one scheduled pass: Run checks once, then sees ctx is done.
func runOnce(s *backup.Service) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
}

func TestScheduledBackupPrunesOnlyAutoBackups(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dataDir := t.TempDir()
	cfg := config.Backup{Enabled: true, Interval: config.Duration(24 * time.Hour), Keep: 2}
	s := service(t, db, dbPath, dataDir, func() config.Backup { return cfg })
	newestOld := seed(t, dataDir, backup.KindAuto, 25*time.Hour)
	seed(t, dataDir, backup.KindAuto, 49*time.Hour)
	seed(t, dataDir, backup.KindAuto, 73*time.Hour)
	manual := seed(t, dataDir, backup.KindManual, 100*time.Hour)

	runOnce(s)

	entries, _ := s.List()
	var auto, kept []string
	for _, e := range entries {
		if e.Kind == backup.KindAuto {
			auto = append(auto, e.Name)
		} else {
			kept = append(kept, e.Name)
		}
	}
	if len(auto) != 2 || auto[1] != newestOld {
		t.Fatalf("auto backups = %v; want a new one and %s", auto, newestOld)
	}
	if !slices.Equal(kept, []string{manual}) {
		t.Fatalf("other backups = %v; want only %s", kept, manual)
	}
}

func TestScheduledBackupWaitsForInterval(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for name, cfg := range map[string]config.Backup{
		"not due":  {Enabled: true, Interval: config.Duration(24 * time.Hour), Keep: 7},
		"disabled": {Enabled: false, Interval: config.Duration(10 * time.Minute), Keep: 7},
	} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			s := service(t, db, dbPath, dataDir, func() config.Backup { return cfg })
			seed(t, dataDir, backup.KindAuto, time.Hour)
			runOnce(s)
			if entries, _ := s.List(); len(entries) != 1 {
				t.Fatalf("got %d backups; want the seeded one only", len(entries))
			}
		})
	}
}

func TestPathRejectsTraversal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := service(t, db, dbPath, t.TempDir(), func() config.Backup { return config.Backup{} })

	for _, name := range []string{
		"../../etc/passwd",
		"manual-.db",
		"manual-2024-01-01.db",
		"nope.db",
		"",
	} {
		if path, err := s.Path(name); err == nil {
			t.Errorf("Path(%q) = %s, nil; want error", name, path)
		}
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := service(t, db, dbPath, t.TempDir(), func() config.Backup { return config.Backup{} })

	e, _ := s.Create(backup.KindManual)
	path, _ := s.Path(e.Name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("before delete: %v", err)
	}
	if err := s.Delete(e.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestImportRefusesMalformedDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "firex.db")
	db, err := store.Open(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := service(t, db, dbPath, t.TempDir(), func() config.Backup { return config.Backup{} })

	r := strings.NewReader("this is not a database")
	if _, err := s.Import(r); err == nil {
		t.Fatal("Import accepted non-database")
	}
}
