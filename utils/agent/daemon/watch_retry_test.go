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

// A "thanks, test is ok" that was queued during quiet hours — before the
// rules learned to refuse thank-yous, or on a ticket that has since been
// closed — is dropped in the morning, not started. Starting it would have
// moved a Done ticket back to In Progress.
func TestADeferredFixOnFinishedWorkIsDroppedInTheMorning(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]*sync.Mutex{"api": {}}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Action: "fix", Interval: time.Minute,
		Rules: watch.Rules{Enabled: true, Comments: true}}
	now := time.Now()
	thanks := watch.Event{Key: "k-thanks", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueComment,
		Body: "Thanks ! test is ok", Mine: true, At: now.Add(-9 * time.Hour)}
	closed := watch.Event{Key: "k-closed", Ref: "ABC-2", Workspace: "api", Kind: watch.KindIssueComment,
		Body: "can you also handle the empty list case?", Mine: true, State: "In Review", At: now.Add(-8 * time.Hour)}
	live := watch.Event{Key: "k-live", Ref: "ABC-3", Workspace: "api", Kind: watch.KindIssueComment,
		Body: "please retry with the new token", Mine: true, State: "In Review", At: now.Add(-7 * time.Hour)}
	for _, e := range []watch.Event{thanks, closed, live} {
		d.watchState.Fixes.Defer(e)
	}
	// Overnight someone closed ABC-2; the daemon saw it.
	if err := watch.LoadStateLog(d.Dir).Set("k-closed", "Done", now); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d.retryDeferred(ctx, spec, now)

	var started []string
	for _, r := range d.watchState.Fixes.RecentFixes("api", 10) {
		started = append(started, r.Ref)
	}
	if len(started) != 1 || started[0] != "ABC-3" {
		t.Fatalf("only the live request starts: %v", started)
	}
	if left := d.watchState.Fixes.DeferredEvents(); len(left) != 0 {
		t.Fatalf("the thank-you and the closed one leave the queue for good, got %d left", len(left))
	}
	d.runs.Wait()
}

// The same check guards a fix that is about to start fresh: what the poll
// saw a minute ago may be Done by now.
func TestAFixDoesNotStartOnATicketThatIsDoneNow(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]*sync.Mutex{"api": {}}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Action: "fix", Interval: time.Minute,
		Rules: watch.Rules{Enabled: true, Comments: true}}
	e := watch.Event{Key: "k-1", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueComment,
		Body: "one more thing: the label", Mine: true, State: "In Review", At: time.Now()}
	if err := watch.LoadStateLog(d.Dir).Set("k-1", "Done", time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := d.startFix(ctx, spec, e); got != "not started: it is done" {
		t.Fatalf("got %q", got)
	}
	if n := len(d.watchState.Fixes.RecentFixes("api", 10)); n != 0 {
		t.Fatalf("nothing ran, got %d", n)
	}
}
