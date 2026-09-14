package daemon

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// slowClaude is a headless run that takes a moment and records when each
// run began and ended (the fix's context is cancelled the moment it is
// done, before its slot is given back).
type span struct{ start, end time.Time }

func slowClaude(t *testing.T) func() []span {
	t.Helper()
	var mu sync.Mutex
	var spans []*span
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		sp := &span{start: time.Now()}
		mu.Lock()
		spans = append(spans, sp)
		mu.Unlock()
		go func() {
			<-ctx.Done()
			mu.Lock()
			sp.end = time.Now()
			mu.Unlock()
		}()
		return exec.CommandContext(ctx, "sh", "-c", "sleep 0.3; echo done")
	}
	t.Cleanup(func() { claudeCommand = prev })
	return func() []span {
		mu.Lock()
		defer mu.Unlock()
		out := make([]span, 0, len(spans))
		for _, sp := range spans {
			out = append(out, *sp)
		}
		return out
	}
}

// overlapped says whether two runs were inside at the same time.
func overlapped(spans []span) bool {
	for i := range spans {
		for j := range spans {
			if i != j && spans[j].start.Before(spans[i].end) && spans[i].start.Before(spans[j].end) {
				return true
			}
		}
	}
	return false
}

func runTwoFixes(t *testing.T, slots int, isolate bool) bool {
	t.Helper()
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	if isolate {
		d.Isolate = func(dir, branch string) ([]string, error) { return []string{dir}, nil }
	}
	spans := slowClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "fix", Slots: slots, Isolate: isolate}}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "one", Mine: true})
	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:ABC-2", Kind: watch.KindIssueNew, Ref: "ABC-2", Title: "two", Mine: true})
	collectNotes(t, notes, "fixed ABC-1", "fixed ABC-2")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := spans()
		if len(got) == 2 && !got[0].end.IsZero() && !got[1].end.IsZero() {
			return overlapped(got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("two runs never both finished: %+v", spans())
	return false
}

// One slot is today: two fixes in one workspace run one after the other.
// Two slots run them side by side — each in its own worktrees, which is
// why more than one slot needs isolation.
func TestSlotsRunFixesSideBySide(t *testing.T) {
	if runTwoFixes(t, 0, false) {
		t.Fatal("no slots set: one at a time")
	}
	if !runTwoFixes(t, 2, true) {
		t.Fatal("two slots with worktrees: side by side")
	}
	if runTwoFixes(t, 2, false) {
		t.Fatal("two slots without worktrees would share one checkout: one at a time")
	}
}

func TestSlotsOf(t *testing.T) {
	for _, c := range []struct {
		slots   int
		isolate bool
		want    int
	}{{0, false, 1}, {1, true, 1}, {3, true, 3}, {3, false, 1}, {-2, true, 1}} {
		if got := slotsOf(WatchSpec{Slots: c.slots, Isolate: c.isolate}); got != c.want {
			t.Errorf("slots %d isolate %v → %d, want %d", c.slots, c.isolate, got, c.want)
		}
	}
}
