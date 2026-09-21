package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAnInterruptedFixIsDeferredNotJustForgotten(t *testing.T) {
	d := testDaemon(t)
	e := watch.Event{Key: "github:acme/api#7:c1", Source: "github", Kind: watch.KindPRComment, Ref: "acme/api#7", Title: "nit", URL: "https://github.com/acme/api/pull/7", Mine: true, At: time.Now()}
	d.appendWatchEvent(e)
	state := watch.LoadState(d.Dir)
	state.MarkSeen(e.Key)
	state.Fixes.StartFor(e, time.Now())

	d.loadWatchFiles()

	deferred := d.watchState.Fixes.DeferredEvents()
	if len(deferred) != 1 || deferred[0].Key != e.Key {
		t.Fatalf("GitHub never re-sends a comment, so the interrupted event itself must be queued: %+v", deferred)
	}
	if d.watchState.IsSeen(e.Key) {
		t.Fatal("the key is unseen so the deferred run may start")
	}
}

func TestRetryHandsASeenEventBackToTheWatch(t *testing.T) {
	d := testDaemon(t)
	ran := fakeClaude(t)
	prev := commentSettle
	commentSettle = 50 * time.Millisecond
	t.Cleanup(func() { commentSettle = prev })
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "fix", SkipPermissions: true}}
	d.startWatches(context.Background())
	t.Cleanup(func() { d.runs.Wait() })
	e := watch.Event{Key: "github:acme/api#7:c1", Source: "github", Kind: watch.KindPRComment, Ref: "acme/api#7", Title: "nit", URL: "https://github.com/acme/api/pull/7", Mine: true, At: time.Now()}
	d.watchState.MarkSeen(e.Key)

	d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionWatch, WatchEvent: &e})
	time.Sleep(200 * time.Millisecond)
	if len(fakeRuns(ran)) != 0 {
		t.Fatal("a seen event is handled once — no run without --retry")
	}

	d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionWatch, WatchEvent: &e, Retry: true})
	waitFor(t, func() bool { return len(fakeRuns(ran)) == 1 })
}
