package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRepoStateReportsBranchDirtyAndDrift(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	origin, clone := originRepo(t, root, "main")

	state, ok := ReadRepoState(clone)
	if !ok {
		t.Fatal("a clone must report repo state")
	}
	if state.Branch != "main" || state.Dirty || state.Head == "" {
		t.Fatalf("state = %+v, want clean main with a head", state)
	}
	if state.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", state.Upstream)
	}

	writeTrackedFile(t, origin, "second\n")
	git(t, origin, "commit", "-am", "second")
	git(t, clone, "fetch", "-q", "origin")

	state, _ = ReadRepoState(clone)
	if state.Behind != 1 || state.Ahead != 0 {
		t.Errorf("ahead/behind = %d/%d, want 0/1", state.Ahead, state.Behind)
	}

	writeTrackedFile(t, clone, "local\n")
	if state, _ = ReadRepoState(clone); !state.Dirty {
		t.Error("uncommitted work must report dirty")
	}
}

func TestReadRepoStateRejectsNonRepo(t *testing.T) {
	if _, ok := ReadRepoState(t.TempDir()); ok {
		t.Error("a plain directory is not a repo")
	}
	if _, ok := ReadRepoState(""); ok {
		t.Error("an empty path is not a repo")
	}
}

func TestRepoChangedSince(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")

	if changed, known := RepoChangedSince(dir, "main"); !known || changed {
		t.Errorf("a repo sitting on main is unchanged: changed=%v known=%v", changed, known)
	}

	git(t, dir, "checkout", "-q", "-b", "feature")
	writeTrackedFile(t, dir, "feature work\n")
	git(t, dir, "commit", "-qam", "feature")

	if changed, known := RepoChangedSince(dir, "main"); !known || !changed {
		t.Errorf("a commit ahead of main is changed: changed=%v known=%v", changed, known)
	}

	if _, known := RepoChangedSince(dir, "no-such-branch"); known {
		t.Error("an unresolvable base must report unknown, not unchanged")
	}
	if _, known := RepoChangedSince(t.TempDir(), "main"); known {
		t.Error("a non-repo must report unknown")
	}
}

func TestRepoChangedSinceCountsUncommittedWork(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, known := RepoChangedSince(dir, "main"); !known || !changed {
		t.Errorf("uncommitted work counts as changed: changed=%v known=%v", changed, known)
	}
}
