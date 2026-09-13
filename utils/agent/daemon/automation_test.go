package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/push"
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
	// The send finishes with a board write; the temp dir must outlive it.
	t.Cleanup(d.swaps.Wait)
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

// A read in a workspace whose policy is reads is answered by the daemon —
// Enter into the iTerm2 tab, nothing pushed, the row counting it; a write
// in the same workspace, a read elsewhere, and a read in a session the
// daemon cannot type into quietly all ring as before.
func TestAReadIsAllowedByTheWorkspacePolicy(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Sessions.OnTransition = d.onSessionTransition
	t.Cleanup(d.swaps.Wait)
	prev := autoAllowDelay
	autoAllowDelay = time.Millisecond
	t.Cleanup(func() { autoAllowDelay = prev })
	var mu sync.Mutex
	var typed, pushed []string
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, _ bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, target.SessionID+":"+text)
		return nil
	}
	d.Push = func(m push.Message) {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, m.Body)
	}
	d.AllowPolicy = func(s sessions.Session) string {
		if s.Cwd == "/tmp/acme" {
			return config.AutoAllowReads
		}
		return ""
	}
	now := time.Now()
	iterm := sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme", ClaudePID: 1, TermProgram: "iTerm.app", TTY: 5, At: now}
	d.Sessions.Apply(iterm)
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Read", Subject: "main.go", Risk: "reads", At: now})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	if typed[0] != "s1:\r" || len(pushed) != 0 {
		t.Fatalf("Enter, quietly: typed %v pushed %v", typed, pushed)
	}
	mu.Unlock()
	if s, _ := d.Sessions.Lookup("s1"); s.AutoAllowed != 1 {
		t.Fatalf("the row counts it: %+v", s)
	}
	// A write, and a Bash read, still ask.
	d.Sessions.Apply(sessions.Event{Name: "PostToolUse", SessionID: "s1", Tool: "Read", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Edit", Subject: "main.go", Risk: "writes", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PostToolUse", SessionID: "s1", Tool: "Edit", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Bash", Subject: "cat main.go", Risk: "reads", At: now})
	// A read in a workspace with no policy, and one in a VS Code terminal.
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s2", Cwd: "/tmp/other", ClaudePID: 2, TermProgram: "iTerm.app", TTY: 6, At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s2", Tool: "Read", Subject: "a.go", Risk: "reads", At: now})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s3", Cwd: "/tmp/acme", ClaudePID: 3, TermProgram: "vscode", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s3", Tool: "Read", Subject: "b.go", Risk: "reads", At: now})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(pushed) == 4 })
	mu.Lock()
	defer mu.Unlock()
	if len(typed) != 1 {
		t.Fatalf("only the one read was answered: %v", typed)
	}
}
