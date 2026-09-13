package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// The two switches a workspace flips once and forgets: a review lands on a
// branch a session owns and the session hears it as its next message; a
// pull request the forge calls ready is merged. Both read the config as it
// is now, so a phone that flipped them needs no daemon restart.

func writeWatchConfig(t *testing.T, d *Daemon, workspace, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d.Dir, "config.yml"), []byte("workspaces:\n  "+workspace+":\n    watch:\n      enabled: true\n"+yaml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAReviewIsHandedToTheSessionOnItsBranch(t *testing.T) {
	d := trackingDaemon(t)
	var mu sync.Mutex
	var typed []string
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, target.SessionID+": "+text)
		return nil
	}
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	writeWatchConfig(t, d, "acme", "      handOver: true\n")
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir, Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "notify"}}
	d.startWatches(context.Background())
	// A session in a terminal, on the branch whose pull request this is.
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, PR: "https://github.com/acme/api/pull/7", At: time.Now()})

	e := watch.Event{Key: "github:acme/api#7:r1", Source: "github", Kind: watch.KindPRReview, Ref: "acme/api#7", Title: "Add retries", Author: "dan", Body: "line 40 is wrong", URL: "https://github.com/acme/api/pull/7#pullrequestreview-1", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)
	got := collectNotes(t, notes, "handed to the session on it")
	if len(got) == 0 {
		t.Fatal("the notification says it was handed over")
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	line := typed[0]
	mu.Unlock()
	if !strings.HasPrefix(line, "s1: Review feedback on acme/api#7 from dan") || !strings.Contains(line, "line 40 is wrong") {
		t.Fatalf("typed into the session: %s", line)
	}
	if h, ok := watch.LoadHands(d.Dir).Get(e.Key); !ok || h.To != "s1" || h.By != "daemon" {
		t.Fatalf("the row carries the mark: %+v %v", h, ok)
	}
	// Once: the same event again is not typed twice.
	d.handOverEvent(context.Background(), d.Watches[0], e)
	mu.Lock()
	n := len(typed)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("handed twice: %v", typed)
	}
}

func TestAReadyPullRequestIsMergedWhenTheWorkspaceSaysSo(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	var merged []string
	d.MergePull = func(_ context.Context, workspace, link string) error {
		merged = append(merged, workspace+" "+link)
		return nil
	}
	writeWatchConfig(t, d, "acme", "      autoMerge: true\n")
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir}
	link := "https://github.com/acme/api/pull/7"
	ready := watch.PullStatus{State: "open", Checks: "passing", Review: "approved", At: time.Now()}
	// Not yet: review pending.
	d.pullChanged(context.Background(), spec, "acme/api#7", link, watch.PullStatus{}, watch.PullStatus{State: "open", Checks: "passing", Review: "pending", At: time.Now()}, false)
	if len(merged) != 0 {
		t.Fatalf("merged too early: %v", merged)
	}
	d.pullChanged(context.Background(), spec, "acme/api#7", link, watch.PullStatus{}, ready, false)
	if len(merged) != 1 || merged[0] != "acme "+link {
		t.Fatalf("merged: %v", merged)
	}
	collectNotes(t, notes, "merged "+link)
	if st, ok := watch.LoadPullLog(d.Dir).Get("acme/api#7"); !ok || st.State != "merged" {
		t.Fatalf("the log says merged: %+v %v", st, ok)
	}
	// The switch off: nothing merges.
	writeWatchConfig(t, d, "acme", "      autoMerge: false\n")
	d.pullChanged(context.Background(), spec, "acme/api#8", "https://github.com/acme/api/pull/8", watch.PullStatus{}, ready, false)
	if len(merged) != 1 {
		t.Fatalf("merged with the switch off: %v", merged)
	}
}
