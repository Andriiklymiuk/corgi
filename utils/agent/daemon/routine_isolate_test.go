package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func routineDaemon(t *testing.T, kind string) (*Daemon, *[]string, WatchSpec) {
	t.Helper()
	d := testDaemon(t)
	var mu sync.Mutex
	var isolated []string
	d.Isolate = func(dir, branch string) ([]string, error) {
		mu.Lock()
		isolated = append(isolated, branch)
		mu.Unlock()
		return []string{dir}, nil
	}
	fakeClaude(t)
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Rules: watch.Rules{Enabled: true}, Action: "fix", Isolate: true,
		Routines: []config.Routine{{Kind: kind, Schedule: "daily 08:30"}}}
	d.Watches = []WatchSpec{spec}
	d.startWatches(context.Background())
	t.Cleanup(func() { d.runs.Wait() })
	return d, &isolated, spec
}

func TestAReadOnlyRoutineRunsInPlaceWithoutWorktrees(t *testing.T) {
	d, isolated, spec := routineDaemon(t, "digest")
	e, _ := RoutineEvent("acme", spec.Routines[0], time.Now())
	d.startRoutine(context.Background(), spec, spec.Routines[0], e)
	d.runs.Wait()
	if len(*isolated) != 0 {
		t.Fatalf("a digest reads; thirteen worktrees a morning for a read is waste: %v", *isolated)
	}
}

func TestARoutineThatWritesStillGetsWorktrees(t *testing.T) {
	d, isolated, spec := routineDaemon(t, "deps")
	e, _ := RoutineEvent("acme", spec.Routines[0], time.Now())
	d.startRoutine(context.Background(), spec, spec.Routines[0], e)
	d.runs.Wait()
	if len(*isolated) != 1 {
		t.Fatalf("deps triage pushes commits, so it stays off the checkout: %v", *isolated)
	}
}
