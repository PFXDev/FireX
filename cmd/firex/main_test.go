package main

import (
	"context"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/config"
)

func TestWaitDueFollowsIntervalChanges(t *testing.T) {
	running := config.Template()
	running.SyncInterval = config.Duration(time.Hour)
	live := config.NewLive(running)
	sync := func(c *config.Config) config.Duration { return c.SyncInterval }

	done := make(chan bool, 1)
	go func() { done <- waitDue(context.Background(), live, time.Now(), sync) }()

	select {
	case <-done:
		t.Fatal("waitDue returned before the interval elapsed")
	case <-time.After(50 * time.Millisecond):
	}
	shorter := *running
	shorter.SyncInterval = config.Duration(10 * time.Millisecond)
	live.Apply(&shorter)
	select {
	case due := <-done:
		if !due {
			t.Fatal("waitDue reported cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("a shorter interval did not cut the wait already in progress")
	}
}

func TestWaitDueStopsWithContext(t *testing.T) {
	live := config.NewLive(config.Template())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitDue(ctx, live, time.Now(), func(c *config.Config) config.Duration { return c.SyncInterval }) {
		t.Fatal("waitDue reported due after its context ended")
	}
}
