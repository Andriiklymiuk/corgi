package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func gitOutputIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func worktreeWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	api := filepath.Join(ws, "api")
	if err := os.MkdirAll(api, 0o755); err != nil {
		t.Fatal(err)
	}
	gitOutputIn(t, api, "init", "-q", "-b", "main")
	gitOutputIn(t, api, "config", "user.email", "test@example.com")
	gitOutputIn(t, api, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(api, "CLAUDE.md"), []byte("api rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutputIn(t, api, "add", ".")
	gitOutputIn(t, api, "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(ws, "corgi-compose.yml"), []byte("services:\n  api:\n    path: ./api\n    manualRun: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestWorktreeAddPutsTheBranchInsideTheWorkspace(t *testing.T) {
	ws := worktreeWorkspace(t)
	inWorkspace(t, ws)
	t.Cleanup(func() { utils.PayloadOnStdout = false })

	var code int
	out := captureStdout(t, func() { _, code = runCLI(t, "worktree", "add", "api", "--branch", "feature/ABC-1/x") })
	if code != 0 {
		t.Fatalf("add: %d %s", code, out)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
		t.Fatalf("stdout must be the dir alone, for W=$(corgi worktree add ...): %q", out)
	}
	dir := ""
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, ".worktrees") && !strings.Contains(line, "→") {
			dir = strings.TrimSpace(line)
		}
	}
	if dir == "" {
		t.Fatalf("no worktree dir printed: %s", out)
	}
	real, _ := filepath.EvalSymlinks(ws)
	if !strings.HasPrefix(dir, ws) && !strings.HasPrefix(dir, real) {
		t.Fatalf("worktree %s is outside the workspace %s", dir, ws)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("the repo's CLAUDE.md is not in the worktree: %v", err)
	}
	if b := gitOutputIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); b != "feature/ABC-1/x" {
		t.Fatalf("on %q", b)
	}
}

func TestWorktreeAddDetachedForAReview(t *testing.T) {
	ws := worktreeWorkspace(t)
	inWorkspace(t, ws)
	t.Cleanup(func() { utils.PayloadOnStdout = false })

	out, code := cli(t, "--json", "worktree", "add", "api", "--detach", "HEAD")
	if code != 0 || !strings.Contains(out, "detached-") {
		t.Fatalf("detach: %d %s", code, out)
	}
}

func TestWorktreeAddRefusesWhatItCannotDo(t *testing.T) {
	ws := worktreeWorkspace(t)
	inWorkspace(t, ws)
	t.Cleanup(func() { utils.PayloadOnStdout = false })

	if out, code := cli(t, "worktree", "add", "web", "--branch", "x"); code != 2 {
		t.Fatalf("unknown service: %d %s", code, out)
	}
	if out, code := cli(t, "worktree", "add", "api"); code != 2 {
		t.Fatalf("no branch or detach: %d %s", code, out)
	}
	if out, code := cli(t, "worktree", "add", "api", "--branch", "x", "--detach", "HEAD"); code != 2 {
		t.Fatalf("both: %d %s", code, out)
	}
}
