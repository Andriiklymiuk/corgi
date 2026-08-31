package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func originRepo(t *testing.T, root, branch string) (origin, clone string) {
	t.Helper()
	origin = filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, origin, "init", "-b", branch)
	writeTrackedFile(t, origin, "a\n")
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-m", "init")

	clone = filepath.Join(root, "clone")
	git(t, root, "clone", origin, clone)
	return origin, clone
}

func TestCheckoutRepoPullsRequestedBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	origin, clone := originRepo(t, root, "main")

	writeTrackedFile(t, origin, "b\n")
	git(t, origin, "commit", "-am", "second")

	result := CheckoutRepo("api", clone, "main", false)
	if result.Status != CheckoutUpdated {
		t.Fatalf("status = %q (%s), want %q", result.Status, result.Message, CheckoutUpdated)
	}
	if result.Branch != "main" || result.Fallback {
		t.Errorf("branch = %q fallback = %v, want main/false", result.Branch, result.Fallback)
	}
}

func TestCheckoutRepoSwitchesBackFromFeatureBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "main")
	git(t, clone, "checkout", "-b", "feature/x")

	result := CheckoutRepo("api", clone, "main", false)
	if result.Status == CheckoutFailed {
		t.Fatalf("failed: %s", result.Message)
	}
	if current, _ := gitOut(clone, gitRevParse, gitAbbrevRef, "HEAD"); current != "main" {
		t.Errorf("HEAD on %q, want main", current)
	}
}

func TestCheckoutRepoFallsBackToDefaultBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "trunk")

	result := CheckoutRepo("api", clone, "main", false)
	if result.Status == CheckoutFailed {
		t.Fatalf("failed: %s", result.Message)
	}
	if result.Branch != "trunk" || !result.Fallback {
		t.Errorf("branch = %q fallback = %v, want trunk/true", result.Branch, result.Fallback)
	}
}

func TestCheckoutRepoWithoutBranchUsesDefault(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "trunk")
	git(t, clone, "checkout", "-b", "feature/x")

	result := CheckoutRepo("api", clone, "", false)
	if result.Branch != "trunk" {
		t.Fatalf("branch = %q (%s), want trunk", result.Branch, result.Message)
	}
	if result.Fallback {
		t.Error("asking for the default branch is not a fallback")
	}
}

func TestCheckoutRepoSkipsDirtyTree(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "main")
	git(t, clone, "checkout", "-b", "feature/x")
	writeTrackedFile(t, clone, "dirty\n")

	result := CheckoutRepo("api", clone, "main", false)
	if result.Status != CheckoutSkipped {
		t.Fatalf("status = %q, want %q", result.Status, CheckoutSkipped)
	}
	if current, _ := gitOut(clone, gitRevParse, gitAbbrevRef, "HEAD"); current != "feature/x" {
		t.Errorf("HEAD moved to %q, a skipped repo must stay put", current)
	}
}

func TestCheckoutRepoAllowDirtyStillSwitches(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "main")
	git(t, clone, "checkout", "-b", "feature/x")
	if err := os.WriteFile(filepath.Join(clone, "untracked-but-fine.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTrackedFile(t, clone, "dirty\n")

	result := CheckoutRepo("api", clone, "main", true)
	if result.Status == CheckoutSkipped {
		t.Fatalf("--allow-dirty must not skip: %s", result.Message)
	}
}

func TestCheckoutRepoSkipsNonRepo(t *testing.T) {
	requireGit(t)
	result := CheckoutRepo("api", t.TempDir(), "main", false)
	if result.Status != CheckoutSkipped {
		t.Fatalf("status = %q, want %q", result.Status, CheckoutSkipped)
	}
}

func TestCheckoutRepoFailsWhenNoBranchAndNoDefault(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "trunk")

	result := CheckoutRepo("api", dir, "main", false)
	if result.Status != CheckoutFailed {
		t.Fatalf("status = %q, want %q", result.Status, CheckoutFailed)
	}
}

func TestCheckoutRepoLocalOnlyRepoHasNothingToPull(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")

	result := CheckoutRepo("api", dir, "main", false)
	if result.Status != CheckoutUpToDate {
		t.Fatalf("status = %q (%s), want %q", result.Status, result.Message, CheckoutUpToDate)
	}
	if result.Message == "" {
		t.Error("a repo with no upstream should say so")
	}
}

func TestDefaultBranchOf(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "trunk")
	if got := DefaultBranchOf(clone); got != "trunk" {
		t.Errorf("clone default = %q, want trunk", got)
	}

	local := filepath.Join(root, "local")
	initRepoOnBranch(t, local, "master")
	if got := DefaultBranchOf(local); got != "master" {
		t.Errorf("local default = %q, want master", got)
	}

	if got := DefaultBranchOf(t.TempDir()); got != "" {
		t.Errorf("non-repo default = %q, want empty", got)
	}
}

func writeTrackedFile(t *testing.T, dir, content string) {
	t.Helper()
	writeRepoFile(t, filepath.Join(dir, "f.txt"), content)
}

func initRepoOnBranch(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-b", branch)
	writeTrackedFile(t, dir, "a\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
}
