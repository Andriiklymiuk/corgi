package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// A fix that waited for budget runs when budget returns: the most urgent
// first, one per round, never a blocked or ignored one, never with --no-retry.
func TestDeferredFixesComeBackOnTheirOwn(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]*sync.Mutex{"api": {}}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Action: "fix", Interval: time.Minute}
	now := time.Now()
	low := watch.Event{Key: "k-low", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueNew, At: now.Add(-2 * time.Hour)}
	urgent := watch.Event{Key: "k-urgent", Ref: "ABC-2", Workspace: "api", Kind: watch.KindIssueNew, Labels: []string{"urgent"}, At: now.Add(-time.Hour)}
	blocked := watch.Event{Key: "k-blocked", Ref: "ABC-3", Workspace: "api", Kind: watch.KindIssueNew, Labels: []string{"p0"}, At: now}
	other := watch.Event{Key: "k-other", Ref: "XYZ-1", Workspace: "web", Kind: watch.KindIssueNew, At: now}
	for _, e := range []watch.Event{low, urgent, blocked, other} {
		d.watchState.Fixes.Defer(e)
	}
	d.watchState.Fixes.Block("api", "ABC-3", "no token", watch.BlockedByPerson, now)

	started := func() []string {
		var out []string
		for _, r := range d.watchState.Fixes.RecentFixes("api", 10) {
			out = append(out, r.Ref)
		}
		return out
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the run itself must not go anywhere; only the start matters

	d.retryDeferred(ctx, WatchSpec{Workspace: "api", Dir: spec.Dir, Action: "fix", NoRetry: true}, now)
	if len(started()) != 0 {
		t.Fatal("--no-retry leaves them to a manual run")
	}
	d.retryDeferred(ctx, WatchSpec{Workspace: "api", Dir: spec.Dir, Action: "notify"}, now)
	if len(started()) != 0 {
		t.Fatal("notify never runs anything")
	}
	d.retryDeferred(ctx, spec, now)
	if got := started(); len(got) != 1 || got[0] != "ABC-2" {
		t.Fatalf("the urgent one first, one per round: %v", got)
	}
	if len(d.watchState.Fixes.DeferredEvents()) != 3 {
		t.Fatal("the started one left the deferred list")
	}
	d.runs.Wait()
	d.releaseFix("api", "ABC-2")
	d.retryDeferred(ctx, spec, now.Add(time.Minute))
	if got := started(); len(got) != 2 || got[0] != "ABC-1" {
		t.Fatalf("then the older normal one, never the blocked one: %v", got)
	}
	d.runs.Wait()
}
