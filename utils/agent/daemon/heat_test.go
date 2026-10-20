package daemon

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestTooHotCachesAReadingForAMinute(t *testing.T) {
	old, oldB := readThermal, readBattery
	defer func() { readThermal, readBattery = old, oldB; resetHeat() }()
	calls := 0
	readBattery = func() (int, bool, float64, bool) { return 80, true, 30, true }
	readThermal = func() (int, bool) { calls++; return 40, true }
	resetHeat()
	now := time.Now()
	if !tooHot(now) || !tooHot(now.Add(10*time.Second)) {
		t.Fatal("a CPU held at 40% is hot")
	}
	if calls != 1 {
		t.Fatalf("second call within a minute must hit the cache, got %d reads", calls)
	}
	readThermal = func() (int, bool) { return 100, true }
	if tooHot(now.Add(2 * time.Minute)) {
		t.Fatal("cooled")
	}
	readThermal = func() (int, bool) { return 0, false }
	readBattery = func() (int, bool, float64, bool) { return 0, false, 0, false }
	if tooHot(now.Add(4 * time.Minute)) {
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

func TestFixDeferralLowBattery(t *testing.T) {
	old := readBattery
	defer func() { readBattery = old; resetHeat() }()
	readBattery = func() (int, bool, float64, bool) { return 15, false, 30, true }
	resetHeat()
	log := watch.LoadFixLog(t.TempDir())
	if got := fixDeferral(WatchSpec{Workspace: "w"}, log, time.Now()); got != "low battery" {
		t.Fatalf("15%% on battery holds new runs, got %q", got)
	}
	readBattery = func() (int, bool, float64, bool) { return 15, true, 30, true }
	resetHeat()
	if got := fixDeferral(WatchSpec{Workspace: "w"}, log, time.Now()); got != "" {
		t.Fatalf("plugged in at 15%% is fine, got %q", got)
	}
}

func TestLowDiskGate(t *testing.T) {
	old := readFreeGB
	defer func() { readFreeGB = old; onLowDisk = nil; resetDisk() }()
	told := 0
	onLowDisk = func() { told++ }
	readFreeGB = func(string) (int, bool) { return 3, true }
	resetDisk()
	now := time.Now()
	if !lowDisk(now, "/x") || told != 1 {
		t.Fatalf("3 GB is low: told=%d", told)
	}
	log := watch.LoadFixLog(t.TempDir())
	if got := fixDeferral(WatchSpec{Workspace: "w", AgentDir: "/x"}, log, now); got != "low disk" {
		t.Fatalf("got %q", got)
	}
	readFreeGB = func(string) (int, bool) { return 80, true }
	if lowDisk(now.Add(2*time.Minute), "/x") {
		t.Fatal("room again")
	}
}
