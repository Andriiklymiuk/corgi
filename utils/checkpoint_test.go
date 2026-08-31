package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureAndRestoreWorkTree(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")

	writeTrackedFile(t, dir, "work in progress\n")
	head, err := RepoHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := CaptureWorkTree(dir, "cp1", "api")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("a dirty tree must produce a capture")
	}
	if content := readTracked(t, dir); content != "work in progress\n" {
		t.Fatalf("capture must not touch the working tree, got %q", content)
	}

	writeTrackedFile(t, dir, "ruined\n")
	if err := RestoreWorkTree(dir, "main", head, sha); err != nil {
		t.Fatal(err)
	}
	if content := readTracked(t, dir); content != "work in progress\n" {
		t.Fatalf("restored content = %q", content)
	}
}

func TestCaptureWorkTreeOnCleanRepoCapturesNothing(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")

	sha, err := CaptureWorkTree(dir, "cp1", "api")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		t.Errorf("a clean tree has nothing to capture, got %q", sha)
	}
}

func TestRestoreWorkTreeMovesBackToTheCheckpointCommit(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")
	head, err := RepoHead(dir)
	if err != nil {
		t.Fatal(err)
	}

	writeTrackedFile(t, dir, "later\n")
	git(t, dir, "commit", "-qam", "later")

	if err := RestoreWorkTree(dir, "main", head, ""); err != nil {
		t.Fatal(err)
	}
	now, _ := RepoHead(dir)
	if now != head {
		t.Errorf("HEAD = %s, want %s", now, head)
	}
}

func TestDropCheckpointRefsReleasesTheCapture(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoOnBranch(t, dir, "main")
	writeTrackedFile(t, dir, "work\n")
	if _, err := CaptureWorkTree(dir, "cp1", "api"); err != nil {
		t.Fatal(err)
	}

	DropCheckpointRefs(dir, "cp1")
	out, _ := gitOut(dir, "for-each-ref", "--format=%(refname)", checkpointRefPrefix+"cp1")
	if out != "" {
		t.Errorf("refs still held after drop: %q", out)
	}
}

func TestRestoreWorkTreeRejectsNonRepo(t *testing.T) {
	if err := RestoreWorkTree(t.TempDir(), "main", "abc", ""); err == nil {
		t.Error("restoring outside a repo must error")
	}
}

func readTracked(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
