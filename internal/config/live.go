package config

import (
	"sync"
	"sync/atomic"
)

// Live is the config the running process acts on. Saving the file swaps in a
// new one, except for the settings bound once at startup — the listener and
// the storage paths — which keep their startup values until a restart.
//
// A *Config handed out by Get is shared and must be treated as read-only.
type Live struct {
	cur     atomic.Pointer[Config]
	mu      sync.Mutex
	changed chan struct{}
}

func NewLive(running *Config) *Live {
	l := &Live{changed: make(chan struct{})}
	l.cur.Store(running)
	return l
}

func (l *Live) Get() *Config { return l.cur.Load() }

// Changed returns a channel that is closed by the next Apply, so a loop
// sleeping on an interval can wake up and re-read it.
func (l *Live) Changed() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changed
}

// Apply adopts every setting of the file form that can change without a
// restart, and returns the ones that still need one.
func (l *Live) Apply(file *Config) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	running := l.cur.Load()
	l.cur.Store(file.hot(running))
	close(l.changed)
	l.changed = make(chan struct{})
	return file.RestartFields(running)
}

// hot is the file form as this process can run it: its own settings, except
// the startup-bound ones, which stay at running's values.
func (c Config) hot(running *Config) *Config {
	c.Listen, c.DataDir, c.DBPath = running.Listen, running.DataDir, running.DBPath
	// The password is a one-shot reset applied at startup, never a setting
	// the running process holds on to.
	c.AdminPassword = ""
	return &c
}

// RestartFields lists, by their JSON keys, the file's settings that differ
// from the running process and only take effect on the next start. A pending
// password reset is one of them even though it has nothing to differ from.
func (c Config) RestartFields(running *Config) []string {
	c.resolve()
	fields := []string{}
	if c.Listen != running.Listen {
		fields = append(fields, "listen")
	}
	if c.DataDir != running.DataDir {
		fields = append(fields, "dataDir")
	}
	if c.DBPath != running.DBPath {
		fields = append(fields, "dbPath")
	}
	if c.AdminPassword != "" {
		fields = append(fields, "adminPassword")
	}
	return fields
}
