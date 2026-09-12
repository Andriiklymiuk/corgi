package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A session of its own: a worktree on corgi/<ref>, under the same base the
// unattended runs use, so one prune finds them all. Without a ticket the
// minute names the branch.
func TestIsolationRefIsTheTicketElseTheMinute(t *testing.T) {
	at := time.Date(2026, 9, 12, 14, 5, 0, 0, time.Local)
	if got := isolationRef("ABC-12,ABC-13", at); got != "ABC-12" {
		t.Fatalf("the first ticket names it: %q", got)
	}
	if got := isolationRef("", at); got != "session-0912-1405" {
		t.Fatalf("no ticket, the minute: %q", got)
	}
}

func TestIsolateWorkspaceGivesABareCheckoutItsOwnWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "one")

	trees, start, err := isolateWorkspace(repo, "corgi/abc-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 1 || start != trees[0] || !strings.Contains(start, filepath.Join("corgi_services", ".worktrees")) {
		t.Fatalf("one worktree under the agent base, and the session starts in it: %v / %s", trees, start)
	}
	out, err := exec.Command("git", "-C", start, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != "corgi/abc-7" {
		t.Fatalf("the worktree is on the branch: %q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(start, "a.txt")); err != nil {
		t.Fatal("the worktree carries the checkout's files")
	}
	// The checkout itself stays where it was.
	out, _ = exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if strings.TrimSpace(string(out)) != "main" {
		t.Fatalf("the main checkout must not move: %q", out)
	}
	// Asking again reuses it rather than making a second.
	again, start2, err := isolateWorkspace(repo, "corgi/abc-7")
	if err != nil || len(again) != 1 || start2 != start {
		t.Fatalf("the same branch is the same worktree: %v %s %v", again, start2, err)
	}
}
