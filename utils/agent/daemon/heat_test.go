package daemon

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestTooHotCachesAndNotifiesOnce(t *testing.T) {
	old := readThermal
	defer func() { readThermal = old; onHot = nil; resetHeat() }()
	calls, hot := 0, 0
	readThermal = func() (int, bool) { calls++; return 40, true }
	onHot = func() { hot++ }
	resetHeat()
	now := time.Now()
	if !tooHot(now) || !tooHot(now.Add(10*time.Second)) {
		t.Fatal("40% is hot")
	}
	if calls != 1 {
		t.Fatalf("second call within a minute must hit the cache, got %d reads", calls)
	}
	if hot != 1 {
		t.Fatalf("one notification, got %d", hot)
	}
	readThermal = func() (int, bool) { return 100, true }
	if tooHot(now.Add(2 * time.Minute)) {
		t.Fatal("cooled")
	}
	readThermal = func() (int, bool) { return 30, true }
	tooHot(now.Add(4 * time.Minute))
	if hot != 2 {
		t.Fatalf("hot again after cooling notifies again, got %d", hot)
	}
	readThermal = func() (int, bool) { return 0, false }
	if tooHot(now.Add(6 * time.Minute)) {
		t.Fatal("unknown is not hot")
	}
}

func TestFixDeferralTooHot(t *testing.T) {
	old := readThermal
	defer func() { readThermal = old; resetHeat() }()
	readThermal = func() (int, bool) { return 10, true }
	resetHeat()
	log := watch.LoadFixLog(t.TempDir())
	if got := fixDeferral(WatchSpec{Workspace: "w"}, log, time.Now()); got != "too hot" {
		t.Fatalf("got %q", got)
	}
}
