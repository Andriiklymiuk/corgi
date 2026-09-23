package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
)

func TestTheMachineStaysAwakeForASessionCorgiDidNotStart(t *testing.T) {
	if anyWorking(nil) {
		t.Fatal("an empty board is not work")
	}
	if !anyWorking([]sessions.Session{{ID: "hand-started", Status: sessions.StatusWorking}}) {
		t.Fatal("a session someone opened themselves counts exactly as much")
	}

	for _, quiet := range []sessions.Status{sessions.StatusDone, sessions.StatusStale, sessions.StatusNeedsInput, sessions.StatusGone} {
		if anyWorking([]sessions.Session{{ID: "s", Status: quiet}}) {
			t.Errorf("%s is not mid-turn - waiting on a person is not work", quiet)
		}
	}
	if !anyWorking([]sessions.Session{
		{ID: "a", Status: sessions.StatusDone},
		{ID: "b", Status: sessions.StatusWorking},
		{ID: "c", Status: sessions.StatusStale},
	}) {
		t.Fatal("one session mid-turn is enough")
	}

	if (&Daemon{}).anyoneWorking() {
		t.Fatal("no board, no lock")
	}
}

func TestTheAwakeGuardStopsWithTheDaemon(t *testing.T) {
	d := &Daemon{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.HoldAwakeWhileWorking(ctx, supervisor.NewWakeLock(supervisor.WakeLockOff))
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a guard nobody can stop is a goroutine leak")
	}

	d.HoldAwakeWhileWorking(ctx, nil)
}
