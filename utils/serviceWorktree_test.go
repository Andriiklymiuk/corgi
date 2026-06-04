package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestBranchSlug(t *testing.T) {
	for in, want := range map[string]string{
		"feature/ABC-123": "feature-ABC-123",
		"main":            "main",
		"a b:c/d":         "a-b-c-d",
	} {
		if got := branchSlug(in); got != want {
			t.Errorf("branchSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorktreeDest(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/ws"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	got := worktreeDest("api", "feature/x")
	want := "/ws/corgi_services/.worktrees/api-feature-x"
	if got != want {
		t.Errorf("worktreeDest = %q, want %q", got, want)
	}
}

func TestCutServicePair(t *testing.T) {
	if n, v, err := cutServicePair("api=feature/x"); err != nil || n != "api" || v != "feature/x" {
		t.Errorf("got (%q,%q,%v)", n, v, err)
	}
	for _, bad := range []string{"api", "=x", "api="} {
		if _, _, err := cutServicePair(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestAssertNoServiceWorkdirConflict(t *testing.T) {
	mk := func(dir, branch, checkout []string) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().StringArray("service-dir", dir, "")
		c.Flags().StringArray("service-branch", branch, "")
		c.Flags().StringArray("service-checkout", checkout, "")
		return c
	}
	if err := assertNoServiceWorkdirConflict(mk([]string{"api=/x"}, []string{"web=feat"}, nil)); err != nil {
		t.Errorf("disjoint should be ok: %v", err)
	}
	if err := assertNoServiceWorkdirConflict(mk([]string{"api=/x"}, []string{"api=feat"}, nil)); err == nil {
		t.Error("api in both dir and branch should conflict")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestEnsureServiceWorktreeReuseDirtyClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "init")
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dest := worktreeDest("api", "feature/x")
	if err := EnsureServiceWorktree(repo, "feature/x", dest); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !insideWorktree(dest) {
		t.Fatal("dest is not a worktree")
	}
	// reuse: second call on the existing healthy worktree must not error
	if err := EnsureServiceWorktree(repo, "feature/x", dest); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	if dirty, _ := isTreeDirty(repo); dirty {
		t.Fatal("clean repo reported dirty")
	}
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := isTreeDirty(repo); !dirty {
		t.Fatal("modified tracked file should be dirty")
	}

	if err := CleanCorgiWorktrees(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after clean")
	}
}
