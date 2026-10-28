// Package backup keeps database snapshots under DataDir/backups: scheduled
// ones the service prunes, and manual, uploaded and pre-restore ones that stay
// until an admin deletes them.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/PFXDev/FireX/internal/config"
	"github.com/PFXDev/FireX/internal/store"
)

// Kind says where a backup came from. Only KindAuto is pruned.
type Kind string

const (
	KindAuto       Kind = "auto"
	KindManual     Kind = "manual"
	KindUpload     Kind = "upload"
	KindPreRestore Kind = "pre-restore"
)

const stampLayout = "20060102-150405"

// namePattern is the only shape a backup name can take, which is also what
// keeps a name from the API from reaching outside the backup directory.
var namePattern = regexp.MustCompile(`^(auto|manual|upload|pre-restore)-(\d{8}-\d{6})(?:-(\d+))?\.db$`)

var ErrNotFound = errors.New("备份不存在")

var ErrRestorePending = errors.New("已有一次还原等待重启，请等 FireX 重启完成")

type Entry struct {
	Name      string `json:"name"`
	Kind      Kind   `json:"kind"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"createdAt"`
}

type Service struct {
	db     *store.DB
	dbPath string
	dir    string
	cfg    func() config.Backup
	logger *log.Logger
	// mu serializes everything that writes into the directory, so a scheduled
	// run never prunes while a manual backup or restore is being written.
	mu sync.Mutex
}

func New(db *store.DB, dbPath, dataDir string, cfg func() config.Backup, logger *log.Logger) *Service {
	return &Service{db: db, dbPath: dbPath, dir: filepath.Join(dataDir, "backups"), cfg: cfg, logger: logger}
}

func (s *Service) Dir() string { return s.dir }

// Path resolves a backup name from the API to its file.
func (s *Service) Path(name string) (string, error) {
	if !namePattern.MatchString(name) {
		return "", ErrNotFound
	}
	path := filepath.Join(s.dir, name)
	if _, err := os.Stat(path); err != nil {
		return "", ErrNotFound
	}
	return path, nil
}

func (s *Service) List() ([]Entry, error) {
	files, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, f := range files {
		m := namePattern.FindStringSubmatch(f.Name())
		if m == nil || !f.Type().IsRegular() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		created, err := time.ParseInLocation(stampLayout, m[2], time.Local)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{Name: f.Name(), Kind: Kind(m[1]), Size: info.Size(), CreatedAt: created.UnixMilli()})
	}
	// Newest first; names break ties so same-second backups keep their order.
	slices.SortFunc(entries, func(a, b Entry) int {
		if a.CreatedAt != b.CreatedAt {
			return int(b.CreatedAt - a.CreatedAt)
		}
		return compareSeq(b.Name, a.Name)
	})
	return entries, nil
}

// Create snapshots the open database as a new backup of kind.
func (s *Service) Create(kind Kind) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.create(kind)
}

func (s *Service) create(kind Kind) (Entry, error) {
	path, err := s.reserve(kind)
	if err != nil {
		return Entry{}, err
	}
	if err := s.db.Snapshot(path); err != nil {
		return Entry{}, fmt.Errorf("备份数据库失败：%w", err)
	}
	return s.entry(path)
}

// Import checks an uploaded database and keeps it as a backup, so restoring it
// goes through the same path as restoring any other.
func (s *Service) Import(r io.Reader) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Entry{}, err
	}
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, r)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Entry{}, fmt.Errorf("接收上传文件失败：%w", err)
	}
	if err := store.CheckBackup(tmp.Name()); err != nil {
		return Entry{}, err
	}
	path, err := s.reserve(KindUpload)
	if err != nil {
		return Entry{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Entry{}, err
	}
	return s.entry(path)
}

func (s *Service) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// StageRestore snapshots the current database first, so the restore itself can
// be undone, then stages name for the next start. It returns the snapshot.
func (s *Service) StageRestore(name string, note store.RestoreNote) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A second restore would overwrite the first, and its failed restart would
	// then cancel the one already on its way.
	if s.PendingRestore() {
		return Entry{}, ErrRestorePending
	}
	path, err := s.Path(name)
	if err != nil {
		return Entry{}, err
	}
	if err := store.CheckBackup(path); err != nil {
		return Entry{}, err
	}
	safety, err := s.create(KindPreRestore)
	if err != nil {
		return Entry{}, err
	}
	note.Backup = name
	if err := store.StageRestore(s.dbPath, path, note); err != nil {
		return Entry{}, err
	}
	return safety, nil
}

func (s *Service) CancelRestore() { store.CancelRestore(s.dbPath) }

func (s *Service) PendingRestore() bool { return store.PendingRestore(s.dbPath) }

// Run takes a scheduled backup whenever the newest one is older than the
// interval, until ctx ends. Going by the files rather than a timer means a
// restart neither adds an extra backup nor resets the wait.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.runDue(); err != nil {
			s.logger.Printf("firex: backup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runDue() error {
	cfg := s.cfg()
	if !cfg.Enabled {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.List()
	if err != nil {
		return err
	}
	auto := slices.DeleteFunc(entries, func(e Entry) bool { return e.Kind != KindAuto })
	if len(auto) > 0 && time.Since(time.UnixMilli(auto[0].CreatedAt)) < cfg.Interval.Duration() {
		return nil
	}
	created, err := s.create(KindAuto)
	if err != nil {
		return err
	}
	s.logger.Printf("firex: backup: wrote %s", created.Name)
	auto = append([]Entry{created}, auto...)
	for _, old := range auto[min(cfg.Keep, len(auto)):] {
		if err := os.Remove(filepath.Join(s.dir, old.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Printf("firex: backup: prune %s: %v", old.Name, err)
		}
	}
	return nil
}

// reserve picks an unused name for a new backup of kind, numbering backups
// that land in the same second.
func (s *Service) reserve(kind Kind) (string, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	stamp := time.Now().Format(stampLayout)
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s-%s.db", kind, stamp)
		if n > 1 {
			name = fmt.Sprintf("%s-%s-%d.db", kind, stamp, n)
		}
		path := filepath.Join(s.dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
	}
}

func (s *Service) entry(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	m := namePattern.FindStringSubmatch(info.Name())
	created, _ := time.ParseInLocation(stampLayout, m[2], time.Local)
	return Entry{Name: info.Name(), Kind: Kind(m[1]), Size: info.Size(), CreatedAt: created.UnixMilli()}, nil
}

// compareSeq orders two names from the same second by their -N suffix.
func compareSeq(a, b string) int {
	seq := func(name string) int {
		m := namePattern.FindStringSubmatch(name)
		if m == nil || m[3] == "" {
			return 1
		}
		n, _ := strconv.Atoi(m[3])
		return n
	}
	return seq(a) - seq(b)
}
