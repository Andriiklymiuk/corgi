package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestARunKilledByTheDaemonStoppingIsNotAFailure(t *testing.T) {
	d := testDaemon(t)
	var mu sync.Mutex
	var rang []string
	d.Notify = func(_, body string) { mu.Lock(); rang = append(rang, body); mu.Unlock() }
	d.NotifyWithLink = func(_, body, _ string) { d.Notify("", body) }
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "5")
	}
	t.Cleanup(func() { claudeCommand = prev })
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "fix"}}
	ctx, stop := context.WithCancel(context.Background())
	d.startWatches(ctx)
	d.handleWatchEvent(ctx, watch.Event{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "one", Mine: true})
	waitFor(t, func() bool { return len(d.watchState.Fixes.RecentFixes("acme", 5)) == 1 })
	time.Sleep(100 * time.Millisecond)

	stop()
	d.runs.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, body := range rang {
		if strings.Contains(body, "failed") {
			t.Fatalf("a restart is not the run's fault, nothing rings: %v", rang)
		}
	}
	recs := d.watchState.Fixes.RecentFixes("acme", 5)
	if len(recs) != 1 || recs[0].Done() {
		t.Fatalf("the record stays open so the next start offers the event again: %+v", recs)
	}
	if d.watchState.Fixes.FailedInARow("acme", "ABC-1") != 0 {
		t.Fatal("an interrupted run must not count toward the breaker")
	}
}

