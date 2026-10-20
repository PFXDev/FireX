package config

import (
	"strings"
	"testing"
	"time"
)

func TestLiveApplyKeepsStartupBoundSettings(t *testing.T) {
	running := Template()
	running.complete()
	running.resolve()
	live := NewLive(running)
	changed := live.Changed()

	file := *Template()
	file.complete()
	file.Listen = ":9090"
	file.DataDir = "./elsewhere"
	file.AdminPassword = "pending-reset-123"
	file.Debug = true
	file.SyncInterval = Duration(30 * time.Second)
	pending := live.Apply(&file)

	if got := strings.Join(pending, ","); got != "listen,dataDir,dbPath,adminPassword" {
		t.Fatalf("Apply() pending = %q", got)
	}
	select {
	case <-changed:
	default:
		t.Fatal("Apply() did not close the Changed channel")
	}
	got := live.Get()
	if got.Listen != running.Listen || got.DataDir != running.DataDir || got.DBPath != running.DBPath || got.AdminPassword != "" {
		t.Fatalf("startup-bound settings moved: %+v", got)
	}
	if !got.Debug || got.SyncInterval != Duration(30*time.Second) {
		t.Fatalf("hot settings not applied: %+v", got)
	}
	if file.Listen != ":9090" || file.AdminPassword == "" {
		t.Fatal("Apply() mutated the file form it was given")
	}
}

func TestRestartFieldsFollowsResolvedDBPath(t *testing.T) {
	running := Template()
	running.complete()
	running.resolve()
	file := *Template()
	file.complete()
	if fields := file.RestartFields(running); len(fields) != 0 {
		t.Fatalf("unchanged file needs restart for %v", fields)
	}
	// Naming the path dataDir already implies is not a change.
	file.DBPath = running.DBPath
	if fields := file.RestartFields(running); len(fields) != 0 {
		t.Fatalf("explicit default dbPath needs restart for %v", fields)
	}
}
