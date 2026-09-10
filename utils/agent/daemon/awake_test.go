package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
)

// The per-workspace lock only covered corgi's own supervised processes, so a
// Claude someone started in a terminal was cut off by the lid closing. Any
// tracked session mid-turn holds the machine awake now.
func TestTheMachineStaysAwakeForASessionCorgiDidNotStart(t *testing.T) {
	if anyWorking(nil) {
		t.Fatal("an empty board is not work")
	}
	// Nothing here says who started it: a terminal session counts as much as
	// one corgi supervises.
	if !anyWorking([]sessions.Session{{ID: "hand-started", Status: sessions.StatusWorking}}) {
		t.Fatal("a session someone opened themselves counts exactly as much")
	}

	for _, quiet := range []sessions.Status{sessions.StatusDone, sessions.StatusStale, sessions.StatusNeedsInput, sessions.StatusGone} {
		if anyWorking([]sessions.Session{{ID: "s", Status: quiet}}) {
			t.Errorf("%s is not mid-turn — waiting on a person is not work", quiet)
		}
	}
	// One busy session among idle ones still holds it.
	if !anyWorking([]sessions.Session{
		{ID: "a", Status: sessions.StatusDone},
		{ID: "b", Status: sessions.StatusWorking},
		{ID: "c", Status: sessions.StatusStale},
	}) {
		t.Fatal("one session mid-turn is enough")
	}

	// A daemon with no board at all must not panic or hold anything.
	if (&Daemon{}).anyoneWorking() {
		t.Fatal("no board, no lock")
	}
}

// The guard must stop with the daemon rather than outliving it.
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

	// No lock is not a crash: a platform without one still supervises.
	d.HoldAwakeWhileWorking(ctx, nil)
}
