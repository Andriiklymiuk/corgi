package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestReviewHoldCountsFromTheRequest(t *testing.T) {
	now := time.Now()
	spec := WatchSpec{ReviewDelay: time.Hour}
	e := watch.Event{Kind: watch.KindReviewRequested, At: now.Add(-10 * time.Minute)}

	at, hold := reviewHold(spec, e, now)
	if !hold || !at.Equal(e.At.Add(time.Hour)) {
		t.Errorf("hold = %v until %v, want held until an hour after the ask", hold, at)
	}
	e.At = now.Add(-2 * time.Hour)
	if _, hold := reviewHold(spec, e, now); hold {
		t.Error("a request older than the delay starts now")
	}
	e.At, e.NotBefore = now, now
	if _, hold := reviewHold(spec, e, now); hold {
		t.Error("a request that already waited is not held again")
	}
	if _, hold := reviewHold(WatchSpec{}, watch.Event{Kind: watch.KindReviewRequested, At: now}, now); hold {
		t.Error("no delay set, no hold: the setting is opt-in")
	}
	if _, hold := reviewHold(spec, watch.Event{Kind: watch.KindPRComment, At: now}, now); hold {
		t.Error("only review requests are held")
	}
}

func TestDeferUntilKeepsTheFirstTime(t *testing.T) {
	l := watch.LoadFixLog(t.TempDir())
	first := time.Now().Add(time.Hour)
	e := watch.Event{Key: "k", Workspace: "acme", Kind: watch.KindReviewRequested}
	l.DeferUntil(e, first)
	if got := l.DeferUntil(e, first.Add(time.Hour)); !got.Equal(first) {
		t.Errorf("asking again moved the hold to %v, want %v", got, first)
	}
	if n := len(l.DeferredEvents()); n != 1 {
		t.Errorf("%d queued, want 1", n)
	}
}

func TestAHeldReviewWaitsThenRunsOnItsOwnTime(t *testing.T) {
	d := testDaemon(t)
	d.watchState = watch.LoadState(d.Dir)
	spec := ticketSpec(t)
	spec.ReviewDelay = time.Hour
	spec.Rules.Reviews = true
	e := watch.Event{Kind: watch.KindReviewRequested, Key: "gh:review:1", Workspace: spec.Workspace, Ref: "acme/web#12", URL: "https://github.com/acme/web/pull/12", At: time.Now(), Author: "a colleague"}

	got := d.startFix(context.Background(), spec, e)
	if !strings.HasPrefix(got, "review held until ") {
		t.Fatalf("startFix = %q, want the review held", got)
	}
	queued := d.watchState.Fixes.DeferredEvents()
	if len(queued) != 1 || queued[0].NotBefore.IsZero() {
		t.Fatalf("queued = %+v, want one event with its time", queued)
	}
	if fixPrompt(queued[0]) == fixPrompt(e) || !strings.Contains(fixPrompt(queued[0]), "as it is now") {
		t.Error("a held review is told to read the pull request as it is now")
	}
}

type reviewedSource struct{ out watch.ReviewOutcome }

func (reviewedSource) Name() string { return "github" }
func (reviewedSource) Poll(context.Context, watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	return nil, nil, nil
}
func (r reviewedSource) MyReviewSince(context.Context, string, time.Time) (watch.ReviewOutcome, bool) {
	return r.out, true
}

func TestAHeldReviewIsDroppedWhenIReviewedInTheMeantime(t *testing.T) {
	d := testDaemon(t)
	spec := ticketSpec(t)
	spec.Rules.Reviews = true
	now := time.Now()
	e := watch.Event{Kind: watch.KindReviewRequested, Key: "gh:review:1", Ref: "acme/web#12", URL: "https://github.com/acme/web/pull/12", At: now.Add(-time.Hour), NotBefore: now}

	spec.Sources = []watch.Source{reviewedSource{out: watch.ReviewOutcome{Approved: true}}}
	if why := d.stillWorthFixing(context.Background(), spec, e); !strings.Contains(why, "already reviewed") {
		t.Errorf("why = %q, want it dropped: I approved while it waited", why)
	}
	spec.Sources = []watch.Source{reviewedSource{}}
	if why := d.stillWorthFixing(context.Background(), spec, e); why != "" {
		t.Errorf("why = %q, want it to run: nothing of mine since the ask", why)
	}
}
