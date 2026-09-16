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

// overlapped says whether two runs were inside at the same time: each run
// sleeps 0.3s, so a second one that started before the first could finish
// ran beside it. Ends are stamped by a goroutine after cancel, too late to
// judge by under load.
func overlapped(spans []span) bool {
	for i := range spans {
		for j := range spans {
			if i != j && spans[j].start.Sub(spans[i].start).Abs() < 250*time.Millisecond {
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

func TestCommentsOnOnePullRequestSettleIntoOneFix(t *testing.T) {
	prev := commentSettle
	commentSettle = 150 * time.Millisecond
	t.Cleanup(func() { commentSettle = prev })
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	spans := slowClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "fix"}}
	d.startWatches(context.Background())
	for i, body := range []string{"rename this", "and add a test", "typo in the docstring"} {
		d.handleWatchEvent(context.Background(), watch.Event{Key: "github:c" + string(rune('1'+i)), Kind: watch.KindPRComment, Ref: "acme/api#7", URL: "https://github.com/acme/api/pull/7", Body: body, Author: "reviewer", Mine: true})
		time.Sleep(50 * time.Millisecond)
	}
	if len(spans()) != 0 {
		t.Fatal("no fix starts while comments are still landing")
	}
	collectNotes(t, notes, "fixed acme/api#7")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(spans()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	if n := len(spans()); n != 1 {
		t.Fatalf("three comments in a row are one fix, got %d", n)
	}
}

func TestACommentDuringAFixGetsAFollowUp(t *testing.T) {
	prev := commentSettle
	commentSettle = 50 * time.Millisecond
	t.Cleanup(func() { commentSettle = prev })
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	spans := slowClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "fix"}}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:c1", Kind: watch.KindPRComment, Ref: "acme/api#7", URL: "https://github.com/acme/api/pull/7", Body: "rename this", Author: "reviewer", Mine: true})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(spans()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.watchState.NewRound()
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:c2", Kind: watch.KindPRComment, Ref: "acme/api#7", URL: "https://github.com/acme/api/pull/7", Body: "one more thing", Author: "reviewer", Mine: true})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(spans()) < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	got := spans()
	if len(got) != 2 || overlapped(got) {
		t.Fatalf("a comment during the fix is one more run after it, got %d overlapped=%v", len(got), len(got) == 2 && overlapped(got))
	}
}
