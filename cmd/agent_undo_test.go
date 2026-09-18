package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestUndoDropsASessionsUncommittedWorkInItsOwnWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	trees := filepath.Join(repo, "corgi_services", ".worktrees")
	_ = os.MkdirAll(trees, 0o755)
	wt := filepath.Join(trees, "APP-1")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "corgi/APP-1", wt, "main")
	_ = os.WriteFile(filepath.Join(wt, "a.go"), []byte("package a\n\nfunc Broken() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wt, "new.go"), []byte("package a\n"), 0o644)

	s := sessions.Session{ID: "s1", Display: "api·APP-1", Cwd: wt, Branch: "corgi/APP-1", Status: sessions.StatusDone}
	plan, err := sessionUndoPlan(s)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Isolated || len(plan.Changed) != 2 {
		t.Fatalf("plan: %+v", plan)
	}
	if err := runSessionUndo(plan, false); err != nil {
		t.Fatal(err)
	}
	if got := gitRun(t, wt, "status", "--porcelain"); got != "" {
		t.Fatalf("clean after undo, got %q", got)
	}
	if data, _ := os.ReadFile(filepath.Join(wt, "a.go")); strings.Contains(string(data), "Broken") {
		t.Fatal("the edit is gone")
	}
	if _, err := os.Stat(filepath.Join(wt, "new.go")); !os.IsNotExist(err) {
		t.Fatal("the new file is gone")
	}

	plan, _ = sessionUndoPlan(s)
	if err := runSessionUndo(plan, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("the worktree is gone")
	}
	if out := gitRun(t, repo, "branch", "--list", "corgi/APP-1"); out != "" {
		t.Fatalf("the branch is gone, got %q", out)
	}

	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // mine\n"), 0o644)
	own := sessions.Session{ID: "s2", Display: "api", Cwd: repo, Branch: "main", Status: sessions.StatusDone}
	if _, err := sessionUndoPlan(own); err == nil || !strings.Contains(err.Error(), "shares your checkout") {
		t.Fatalf("a shared checkout is refused: %v", err)
	}
	busy := s
	busy.Status = sessions.StatusWorking
	if _, err := sessionUndoPlan(busy); err == nil || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("a working session: %v", err)
	}
}
