package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A branch measured against main: how many commits main gained since the
// branch left, and which files a rebase would stop on.
func TestBehindCountsMainAndNamesConflicts(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n")
	write("b.go", "package b\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "checkout", "-qb", "feature")
	write("a.go", "package a // feature\n")
	gitIn(t, dir, "commit", "-qam", "feature work")
	if commits, conflicts, up, ok := gitBehind(dir); !ok || commits != 0 || len(conflicts) != 0 || up != "main" {
		t.Fatalf("nothing moved yet: %d %v %s %v", commits, conflicts, up, ok)
	}
	// Main moves on another file: behind, no conflict.
	gitIn(t, dir, "checkout", "-q", "main")
	write("b.go", "package b // main\n")
	gitIn(t, dir, "commit", "-qam", "main moves b")
	gitIn(t, dir, "checkout", "-q", "feature")
	if commits, conflicts, _, ok := gitBehind(dir); !ok || commits != 1 || len(conflicts) != 0 {
		t.Fatalf("one commit, no conflict: %d %v %v", commits, conflicts, ok)
	}
	// Main touches the feature's file too: a conflict, named.
	gitIn(t, dir, "checkout", "-q", "main")
	write("a.go", "package a // main\n")
	gitIn(t, dir, "commit", "-qam", "main moves a")
	gitIn(t, dir, "checkout", "-q", "feature")
	commits, conflicts, _, ok := gitBehind(dir)
	if !ok || commits != 2 || len(conflicts) != 1 || conflicts[0] != "a.go" {
		t.Fatalf("two commits, a.go conflicts: %d %v %v", commits, conflicts, ok)
	}
	if got := sessions.BehindLine(&sessions.Behind{Commits: commits, Conflicts: conflicts}); got != "main moved 2 · conflicts in a.go" {
		t.Fatalf("line: %q", got)
	}
}

// A session that stops behind main is rebased where it sits when the
// workspace says so and nothing is in the way; one that would conflict is
// told which files under hand-over. Each once per state.
func TestAStoppedSessionBehindMainIsRebasedOrTold(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Sessions.OnTransition = d.onSessionTransition
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	t.Cleanup(d.swaps.Wait)
	var mu sync.Mutex
	var typed, rebased []string
	d.TypeText = func(_ context.Context, _ sessions.FocusTarget, text string, _ bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, text)
		return nil
	}
	prevClean, prevRebase := gitClean, gitRebase
	gitClean = func(string) bool { return true }
	gitRebase = func(_ context.Context, dir, upstream string) error {
		mu.Lock()
		defer mu.Unlock()
		rebased = append(rebased, dir+" onto "+upstream)
		return nil
	}
	t.Cleanup(func() { gitClean, gitRebase = prevClean, prevRebase })
	policy := Policy{Rebase: true, HandOver: true}
	d.Policy = func(sessions.Session) Policy { return policy }
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme", ClaudePID: 1, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.SetBehind("s1", &sessions.Behind{Commits: 3, Upstream: "origin/main", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.swaps.Wait()
	mu.Lock()
	if len(rebased) != 1 || rebased[0] != "/tmp/acme onto origin/main" || len(typed) != 0 {
		t.Fatalf("rebased quietly: %v %v", rebased, typed)
	}
	mu.Unlock()
	s, _ := d.Sessions.Lookup("s1")
	if s.Behind == nil || !s.Behind.Told || s.Behind.Rebased != 1 {
		t.Fatalf("told and counted: %+v", s.Behind)
	}
	// The same state again: nothing. The sweep says the same numbers: still nothing.
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	d.swaps.Wait()
	if d.Sessions.SetBehind("s1", &sessions.Behind{Commits: 3, Upstream: "origin/main", At: now}) {
		t.Fatal("same numbers are not a change")
	}
	// Main moves again, this time into the session's files: told, not rebased.
	d.Sessions.SetBehind("s1", &sessions.Behind{Commits: 5, Conflicts: []string{"api/x.go"}, Upstream: "origin/main", At: now})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", At: now})
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", At: now})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(typed[0], "Main moved: origin/main is 5 commits past your base and would conflict in api/x.go.") || len(rebased) != 1 {
		t.Fatalf("told: %q rebased %v", typed[0], rebased)
	}
	s, _ = d.Sessions.Lookup("s1")
	if s.Behind.Rebased != 1 || !s.Behind.Told {
		t.Fatalf("the count survives a new state: %+v", s.Behind)
	}
}
