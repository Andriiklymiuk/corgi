package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestARedBuildIsRerunOnceBeforeItIsWorked(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	reruns := 0
	d.RerunCI = func(_ context.Context, ws, repo string, since time.Time) (watch.Rerun, error) {
		if ws != "acme" || repo != "acme/api" {
			t.Fatalf("asked about %s %s", ws, repo)
		}
		reruns++
		if reruns > 1 {
			return watch.Rerun{}, errors.New("run 12 was rerun already")
		}
		return watch.Rerun{RunID: 12, URL: "https://github.com/acme/api/actions/runs/12", At: time.Now()}, nil
	}
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Repos: []string{"acme/api"},
		Rules: watch.Rules{Enabled: true, CI: true}, Action: "fix", FixKinds: []string{"ci.failed"}, RerunCI: true}}
	d.startWatches(context.Background())

	now := time.Now()
	first := watch.Event{Key: "github:ci:acme/api:1:" + now.Format(time.RFC3339), Kind: watch.KindCIFailed, Ref: "acme/api", Title: "ci failed", Mine: true, At: now}
	d.handleWatchEvent(context.Background(), first)
	got := collectNotes(t, notes, "rerunning its failed jobs once")
	if len(got) != 1 || len(*ran) != 0 {
		t.Fatalf("the first red is rerun, not worked: notes %v runs %v", got, *ran)
	}
	d.watchState.NewRound()
	second := watch.Event{Key: "github:ci:acme/api:1:" + now.Add(5*time.Minute).Format(time.RFC3339), Kind: watch.KindCIFailed, Ref: "acme/api", Title: "ci failed", Mine: true, At: now.Add(5 * time.Minute)}
	d.handleWatchEvent(context.Background(), second)
	collectNotes(t, notes, "fixed acme/api")
	if len(*ran) != 1 {
		t.Fatalf("the second red is worked: runs %v", *ran)
	}
	if reruns != 2 {
		t.Fatalf("asked twice, rerun once: %d", reruns)
	}
}

func TestARedBuildIsNotRerunWithoutTheSwitch(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	asked := false
	d.RerunCI = func(context.Context, string, string, time.Time) (watch.Rerun, error) {
		asked = true
		return watch.Rerun{RunID: 1}, nil
	}
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Repos: []string{"acme/api"},
		Rules: watch.Rules{Enabled: true, CI: true}, Action: "fix", FixKinds: []string{"ci.failed"}}}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:ci:acme/api:1:x", Kind: watch.KindCIFailed, Ref: "acme/api", Title: "ci failed", Mine: true, At: time.Now()})
	collectNotes(t, notes, "fixed acme/api")
	if asked || len(*ran) != 1 {
		t.Fatalf("off: worked at once, never rerun (asked %v, runs %v)", asked, *ran)
	}
}
