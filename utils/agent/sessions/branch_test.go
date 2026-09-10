package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranchReadsHeadAndFollowsWorktreePointers(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "worktrees", "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/fix/login\n"), 0o644)
	deep := filepath.Join(root, "services", "api")
	os.MkdirAll(deep, 0o755)
	if got := Branch(deep); got != "fix/login" {
		t.Fatalf("branch from a subdirectory = %q", got)
	}
	if got := RepoRoot(deep); got != root {
		t.Fatalf("root from a subdirectory = %q", got)
	}

	wt := filepath.Join(root, "wt")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(gitDir, "worktrees", "wt")+"\n"), 0o644)
	os.WriteFile(filepath.Join(gitDir, "worktrees", "wt", "HEAD"), []byte("0123456789abcdef0123\n"), 0o644)
	if got := Branch(wt); got != "0123456789ab" {
		t.Fatalf("detached worktree = %q", got)
	}
	if got := Branch(t.TempDir()); got != "" {
		t.Fatalf("outside a repo = %q", got)
	}
}
