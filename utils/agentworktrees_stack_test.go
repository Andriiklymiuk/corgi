package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stackTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "first"},
		{"branch", "corgi/task-1"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "slice.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"checkout", "-q", "corgi/task-1"},
		{"add", "slice.txt"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "slice one"},
		{"checkout", "-q", "main"},
		{"branch", "corgi/task-2"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return repo
}

func TestABranchStartsFromTheWorkItBuildsOn(t *testing.T) {
	repo := stackTestRepo(t)
	stacked, ok := StackBranchOnBasesInRepo(repo, "corgi/task-3", []string{"corgi/task-9", "corgi/task-1", "corgi/task-2"})
	if !ok || stacked.Base != "corgi/task-1" || strings.Join(stacked.Also, ",") != "corgi/task-2" {
		t.Fatalf("stacked on the first base this repository has: %+v ok=%v", stacked, ok)
	}
	base, _ := gitOut(repo, "rev-parse", "corgi/task-1")
	got, _ := gitOut(repo, "rev-parse", "corgi/task-3")
	if base == "" || base != got {
		t.Fatalf("the new branch points at the base: %q %q", base, got)
	}
	if _, again := StackBranchOnBasesInRepo(repo, "corgi/task-3", []string{"corgi/task-1"}); again {
		t.Fatal("a branch that exists is left alone")
	}
	if _, none := StackBranchOnBasesInRepo(repo, "corgi/task-4", []string{"corgi/task-9"}); none {
		t.Fatal("no base here, nothing stacked")
	}
	if localBranchExists(repo, "corgi/task-4") {
		t.Fatal("no branch is made without a base")
	}
	if _, bad := StackBranchOnBasesInRepo(repo, "corgi/task-5", []string{"--upload-pack=x"}); bad {
		t.Fatal("a base that is not a branch name is refused")
	}
}
