package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestReviewsOnEveryPullRequestOfAStoryAreOneRun(t *testing.T) {
	prev := commentSettle
	commentSettle = 300 * time.Millisecond
	t.Cleanup(func() { commentSettle = prev })
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api", "acme/web"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "fix"}}
	d.startWatches(context.Background())
	t.Cleanup(func() { d.runs.Wait() })

	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:acme/api#526:r1", Kind: watch.KindPRComment, Ref: "acme/api#526", URL: "https://github.com/acme/api/pull/526", Title: "Premium frequency [ABC-1500]", Body: "## Code review — ABC-1500 · api #526\n\n**Verdict: REQUEST_CHANGES — 2 blockers.**", Author: "reviewer", Mine: true})
	time.Sleep(50 * time.Millisecond)
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:acme/web#455:r1", Kind: watch.KindPRComment, Ref: "acme/web#455", URL: "https://github.com/acme/web/pull/455", Title: "Premium frequency [ABC-1500]", Body: "## Code review — ABC-1500 · web #455\n\n**Verdict: REQUEST_CHANGES — 1 blocker.**", Author: "reviewer", Mine: true})

	collectNotes(t, notes, "fixed acme/api#526 + acme/web#455")
	runs := fakeRuns(ran)
	if len(runs) != 1 {
		t.Fatalf("one story, one run over every pull request: %v", runs)
	}
	if !strings.Contains(runs[0], "/corgi:review ABC-1500") || !strings.Contains(runs[0], "pull/526") || !strings.Contains(runs[0], "pull/455") {
		t.Fatalf("the run addresses the set by story id, naming each pull request: %q", runs[0])
	}
	if !strings.Contains(runs[0], "conflicts with its base") {
		t.Fatalf("the conflict step stays: %q", runs[0])
	}
	d.runs.Wait()
	recs := d.watchState.Fixes.RecentFixes("acme", 5)
	if len(recs) != 2 || !recs[0].Done() || !recs[1].Done() {
		t.Fatalf("both pull requests have a finished record: %+v", recs)
	}
}

func TestFeedbackWithoutAStoryStaysPerPullRequest(t *testing.T) {
	if got := storyOf(watch.Event{Kind: watch.KindPRComment, Ref: "acme/api#7", Title: "fix typo"}); got != "" {
		t.Fatalf("no ticket in the title, no story: %q", got)
	}
	if got := storyOf(watch.Event{Kind: watch.KindPRComment, Ref: "acme/api#7", Title: "[HUM-1500] Premium frequency"}); got != "HUM-1500" {
		t.Fatalf("the ticket in the title is the story: %q", got)
	}
	if got := storyOf(watch.Event{Kind: watch.KindIssueNew, Ref: "HUM-1", Title: "[HUM-1500] x"}); got != "" {
		t.Fatal("only review feedback rides by story")
	}
}

func TestALimitCeilingLetsAWorkspaceRunPastTheDefault(t *testing.T) {
	log := watch.LoadFixLog(t.TempDir())
	now := time.Now()
	prev := readUsageLimits
	readUsageLimits = func(string, time.Time) (int, bool) { return 97, true }
	t.Cleanup(func() { readUsageLimits = prev })
	spec := WatchSpec{Workspace: "w", ConfigDir: t.TempDir(), MaxFixesPerHour: 10, MaxFixesPerDay: 10}
	if got := fixDeferral(spec, log, now); got != "limit 97%" {
		t.Fatalf("by default 95%% is the wall: %q", got)
	}
	spec.LimitCeiling = 100
	if got := fixDeferral(spec, log, now); got != "" {
		t.Fatalf("a ceiling of 100 never waits on usage: %q", got)
	}
	spec.LimitCeiling = 98
	if got := fixDeferral(spec, log, now); got != "" {
		t.Fatalf("97 is under a ceiling of 98: %q", got)
	}
	readUsageLimits = func(string, time.Time) (int, bool) { return 99, true }
	if got := fixDeferral(spec, log, now); got != "limit 99%" {
		t.Fatalf("99 is over a ceiling of 98: %q", got)
	}
}
