package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// A routine due on the clock starts through the fix runner, once, and its
// report lands in the inbox as one row with the run's headline.
func TestRoutinesRunOnTheClockAndReportToTheInbox(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]*sync.Mutex{"api": {}}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Action: "notify",
		Routines: []config.Routine{{Name: "digest", Kind: "digest", Schedule: "daily 08:30"}, {Name: "off", Kind: "deps", Schedule: "daily 08:30", Off: true}}}
	d.Watches = []WatchSpec{spec}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local)
	d.runRoutines(ctx, day.Add(8*time.Hour))
	if len(d.watchState.Fixes.RecentFixes("api", 10)) != 0 {
		t.Fatal("not before 08:30")
	}
	d.runRoutines(ctx, day.Add(9*time.Hour))
	d.runs.Wait()
	runs := d.watchState.Fixes.RecentFixes("api", 10)
	if len(runs) != 1 || runs[0].Ref != "routine/digest" {
		t.Fatalf("the due one starts, the off one does not: %+v", runs)
	}
	d.releaseFix("api", "routine/digest")
	d.runRoutines(ctx, day.Add(10*time.Hour))
	d.runs.Wait()
	if len(d.watchState.Fixes.RecentFixes("api", 10)) != 1 {
		t.Fatal("once a day")
	}
	if last := loadRoutineState(d.Dir).get("api/digest"); !last.Equal(day.Add(9 * time.Hour)) {
		t.Fatalf("the start is remembered on disk: %v", last)
	}

	e, ok := RoutineEvent("api", config.Routine{Name: "digest", Kind: "digest"}, day)
	if !ok || !strings.Contains(e.Body, "morning digest") || e.Ref != "routine/digest" {
		t.Fatalf("event from the catalog: %+v", e)
	}
	d.routineReport(spec, e, "3 PRs green, 1 red\n- details…", nil)
	rows := watch.RecentEvents(d.Dir, 5)
	// Two rows: the cancelled run above reported its failure, and this one.
	if len(rows) != 2 || rows[0].Kind != watch.KindRoutine || !strings.Contains(rows[0].Title, "digest — 3 PRs green, 1 red") || rows[0].Body != "" {
		t.Fatalf("inbox row: %+v", rows)
	}
	if !strings.Contains(rows[1].Title, "failed:") {
		t.Fatalf("a failed run reports too: %+v", rows[1])
	}
	if _, ok := RoutineEvent("api", config.Routine{Name: "x"}, day); ok {
		t.Fatal("no kind, no prompt, nothing to run")
	}
}
