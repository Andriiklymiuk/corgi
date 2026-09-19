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
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func writeWatchConfig(t *testing.T, d *Daemon, workspace, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d.Dir, "config.yml"), []byte("workspaces:\n  "+workspace+":\n    watch:\n      enabled: true\n"+yaml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAReviewIsHandedToTheSessionOnItsBranch(t *testing.T) {
	d := trackingDaemon(t)
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
	writeWatchConfig(t, d, "acme", "      autoMerge: false\n")
	d.pullChanged(context.Background(), spec, "acme/api#8", "https://github.com/acme/api/pull/8", watch.PullStatus{}, ready, false)
	if len(merged) != 1 {
		t.Fatalf("merged with the switch off: %v", merged)
	}
}

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
	d.Policy = func(s sessions.Session) Policy {
		if s.Cwd == "/tmp/acme" {
			return Policy{AutoAllow: config.AutoAllowReads}
		}
		return Policy{}
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
	d.Sessions.Apply(sessions.Event{Name: "PostToolUse", SessionID: "s1", Tool: "Read", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Edit", Subject: "main.go", Risk: "writes", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PostToolUse", SessionID: "s1", Tool: "Edit", At: now})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Bash", Subject: "cat main.go", Risk: "reads", At: now})
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

func TestAStopIsGatedByTheWorkspacesDoneWhen(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Sessions.OnTransition = d.onSessionTransition
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	t.Cleanup(func() { d.runs.Wait(); d.swaps.Wait() })
	var mu sync.Mutex
	var typed []string
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, text)
		return nil
	}
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	var policy Policy
	d.Policy = func(sessions.Session) Policy { return policy }
	policy = Policy{DoneWhen: []string{"echo ok", "echo boom; echo bang; exit 1"}}
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: t.TempDir(), ClaudePID: 1, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "PreToolUse", SessionID: "s1", Tool: "Edit", Subject: "main.go", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.runs.Wait()
	if s, _ := d.Sessions.Lookup("s1"); s.Gate != nil {
		t.Fatalf("no work, no gate: %+v", s.Gate)
	}
	d.Sessions.SetChanges("s1", &sessions.Changes{Files: 2, Lines: 40, At: now}, nil)
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	msg := typed[0]
	mu.Unlock()
	if !strings.HasPrefix(msg, "Not done yet: `echo boom; echo bang; exit 1` failed") || !strings.Contains(msg, "boom\nbang") || !strings.Contains(msg, "stop when it is green") {
		t.Fatalf("typed back: %q", msg)
	}
	s, _ := d.Sessions.Lookup("s1")
	if s.Gate == nil || s.Gate.OK || s.Gate.Fails != 1 || s.Tests == nil || s.Tests.OK || s.Tests.Cmd != "echo boom; echo bang; exit 1" {
		t.Fatalf("the row says red: gate %+v tests %+v", s.Gate, s.Tests)
	}
	policy = Policy{DoneWhen: []string{"true"}}
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.runs.Wait()
	s, _ = d.Sessions.Lookup("s1")
	if s.Gate == nil || !s.Gate.OK || s.Gate.Fails != 0 {
		t.Fatalf("green: %+v", s.Gate)
	}
	mu.Lock()
	n := len(typed)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("green types nothing: %v", typed)
	}
	policy = Policy{DoneWhen: []string{"exit 1"}}
	for i := 0; i < 4; i++ {
		d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
		d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
		d.runs.Wait()
	}
	got := collectNotes(t, notes, "still red after 4 tries")
	if len(got) == 0 {
		t.Fatal("the fourth red rings")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(typed) != 4 {
		t.Fatalf("three typed back, the fourth rang: %d", len(typed))
	}
}

func TestAFullSessionIsCompactedWhenItStops(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Sessions.OnTransition = d.onSessionTransition
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	t.Cleanup(d.swaps.Wait)
	var mu sync.Mutex
	var typed []string
	d.TypeText = func(_ context.Context, _ sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, text)
		return nil
	}
	d.Policy = func(sessions.Session) Policy { return Policy{CompactAt: 85} }
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme", ClaudePID: 1, TermProgram: "iTerm.app", TTY: 5, At: now})
	post := sessions.Event{Name: "PostToolUse", SessionID: "s1", Tool: "Read", At: now}
	post.Context = &usage.Context{Tokens: 120_000, Window: 200_000, Percent: 60}
	d.Sessions.Apply(post)
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.swaps.Wait()
	mu.Lock()
	n := len(typed)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("60%% is fine: %v", typed)
	}
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	post.Context = &usage.Context{Tokens: 180_000, Window: 200_000, Percent: 90}
	d.Sessions.Apply(post)
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	first := typed[0]
	mu.Unlock()
	if first != "/compact" {
		t.Fatalf("typed %q", first)
	}
	if s, _ := d.Sessions.Lookup("s1"); s.Compacted != 1 || s.CompactedAt.IsZero() {
		t.Fatalf("counted: %+v", s)
	}
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	d.Sessions.Apply(post)
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.swaps.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(typed) != 1 {
		t.Fatalf("once per episode: %v", typed)
	}
}

func TestAReviewOnMyPullRequestBecomesALesson(t *testing.T) {
	d := trackingDaemon(t)
	d.Notify = func(_, _ string) {}
	writeWatchConfig(t, d, "acme", "      lessons: true\n")
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir, Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}, Action: "notify"}}
	d.startWatches(context.Background())
	e := watch.Event{Key: "github:acme/api#7:r1", Source: "github", Kind: watch.KindPRReview, Ref: "acme/api#7", Title: "Add retries", Author: "dan", Body: "retries need a cap\nand a jitter", URL: "https://github.com/acme/api/pull/7", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)
	d.handleWatchEvent(context.Background(), e)
	got := lessons.List(d.Dir, "acme")
	if len(got) != 1 || got[0].Source != "pr.review acme/api#7 (dan)" || got[0].Text != "retries need a cap" {
		t.Fatalf("%+v", got)
	}
	theirs := e
	theirs.Key, theirs.Mine, theirs.Body = "github:acme/api#8:r1", false, "use a queue"
	d.handleWatchEvent(context.Background(), theirs)
	if got := lessons.List(d.Dir, "acme"); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestTheDailyDigestIsPushedToThePhone(t *testing.T) {
	d := testDaemon(t)
	d.DigestAt = "00:00"
	d.Digest = func(time.Time) string { return "one\ntwo\nthree\nfour\nfive" }
	d.Notify = func(_, _ string) {}
	got := make(chan push.Message, 2)
	d.Push = func(m push.Message) { got <- m }
	d.sendDigestIfDue(time.Now())
	select {
	case m := <-got:
		if m.Category != "brief" || m.Body != "one\ntwo\nthree\n…" || m.Data["brief"] != "1" {
			t.Fatalf("%+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no push")
	}
	d.sendDigestIfDue(time.Now())
	select {
	case m := <-got:
		t.Fatalf("twice: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAMuteHoldsEveryRing(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Sessions.OnTransition = d.onSessionTransition
	var mu sync.Mutex
	var rang []string
	d.Notify = func(title, body string) { mu.Lock(); rang = append(rang, "toast "+body); mu.Unlock() }
	d.Push = func(m push.Message) { mu.Lock(); rang = append(rang, "push "+m.Body); mu.Unlock() }
	if err := SetMute(d.Dir, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	d.notifyAttentionAt("corgi agent · acme", "a new bug", "acme", "")
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 1, At: time.Now()})
	d.Sessions.Apply(sessions.Event{Name: "PermissionRequest", SessionID: "s1", Tool: "Bash", Subject: "go test", At: time.Now()})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(rang) != 0 {
		t.Fatalf("muted, yet: %v", rang)
	}
	mu.Unlock()
	if st := d.Sessions.Snapshot(time.Now()); st.MutedUntil.IsZero() {
		t.Fatal("the board says muted")
	}
	if err := SetMute(d.Dir, time.Time{}); err != nil {
		t.Fatal(err)
	}
	d.notifyAttentionAt("corgi agent · acme", "another bug", "acme", "")
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(rang) == 2 })
	if st := d.Sessions.Snapshot(time.Now()); !st.MutedUntil.IsZero() {
		t.Fatal("the board rings again")
	}
	_ = SetMute(d.Dir, time.Now().Add(-time.Minute))
	if !MutedUntil(d.Dir).IsZero() {
		t.Fatal("passed")
	}
}

func TestAMergedStoryMovesItsTicketOnceEveryPullIsIn(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	var moved []string
	d.MoveTicket = func(_ context.Context, workspace, ref, status string) error {
		moved = append(moved, workspace+" "+ref+" → "+status)
		return nil
	}
	writeWatchConfig(t, d, "acme", "      afterMerge: Ready for QA\n      afterMergeSubtasks: Done\n")
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir}
	fixes := watch.LoadFixLog(d.Dir)
	now := time.Now()
	story := watch.Event{Key: "jira:ABC-7", Workspace: "acme", Ref: "ABC-7", Kind: watch.KindIssueNew, At: now}
	fixes.StartFor(story, now)
	fixes.Finish(story.Key, []string{"https://github.com/acme/api/pull/7", "https://github.com/acme/web/pull/3"}, "", "", now)
	sub := watch.Event{Key: "jira:ABC-8", Workspace: "acme", Ref: "ABC-8", Kind: watch.KindIssueNew, Parent: "ABC-7", At: now}
	fixes.StartFor(sub, now)
	fixes.Finish(sub.Key, []string{"https://github.com/acme/api/pull/9"}, "", "", now)
	d.watchState = watch.LoadState(d.Dir)
	d.watchState.Fixes = fixes

	merged := watch.PullStatus{State: "merged", Checks: "passing", Review: "approved", At: now}
	open := watch.PullStatus{State: "open", Checks: "passing", Review: "approved", At: now}
	d.pullChanged(context.Background(), spec, "acme/api#7", "https://github.com/acme/api/pull/7", open, merged, true)
	if len(moved) != 0 {
		t.Fatalf("one of two pulls merged is not done: %v", moved)
	}
	_ = watch.LoadPullLog(d.Dir).Set("acme/api#7", merged)
	d.pullChanged(context.Background(), spec, "acme/web#3", "https://github.com/acme/web/pull/3", open, merged, true)
	if len(moved) != 1 || moved[0] != "acme ABC-7 → Ready for QA" {
		t.Fatalf("the story moves when its last pull merges: %v", moved)
	}
	collectNotes(t, notes, "ABC-7 → Ready for QA")
	d.pullChanged(context.Background(), spec, "acme/web#3", "https://github.com/acme/web/pull/3", merged, merged, true)
	if len(moved) != 1 {
		t.Fatalf("moved twice: %v", moved)
	}
	d.pullChanged(context.Background(), spec, "acme/api#9", "https://github.com/acme/api/pull/9", open, merged, true)
	if len(moved) != 2 || moved[1] != "acme ABC-8 → Done" {
		t.Fatalf("a subtask goes to its own column: %v", moved)
	}
}
