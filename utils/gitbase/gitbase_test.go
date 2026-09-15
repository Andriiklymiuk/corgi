package gitbase

import (
	"os/exec"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A repo whose default branch is trunk, with a stale local master far
// behind: the base is origin/trunk, not master.
func TestOriginHeadWinsOverStaleMaster(t *testing.T) {
	remote := t.TempDir()
	git(t, remote, "init", "-q", "-b", "trunk")
	git(t, remote, "commit", "-q", "--allow-empty", "-m", "one")
	git(t, remote, "branch", "master")
	git(t, remote, "commit", "-q", "--allow-empty", "-m", "two")
	git(t, remote, "commit", "-q", "--allow-empty", "-m", "three")

	dir := t.TempDir()
	git(t, dir, "clone", "-q", remote, ".")
	git(t, dir, "remote", "set-head", "origin", "trunk")
	git(t, dir, "branch", "master", "origin/master")

	refs := Refs(dir)
	if refs[0] != "origin/trunk" {
		t.Fatalf("first candidate = %q, want origin/trunk (%v)", refs[0], refs)
	}
	base, ref := MergeBase(dir)
	if ref != "origin/trunk" {
		t.Fatalf("merge base taken from %q, want origin/trunk", ref)
	}
	if base != git(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("on trunk itself the base is HEAD; got %s", base)
	}
}

func TestNoOriginFallsBackToLocalNames(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "one")
	if refs := Refs(dir); refs[0] != "origin/main" || refs[len(refs)-1] != "develop" {
		t.Fatalf("refs = %v", refs)
	}
	if _, ref := MergeBase(dir); ref != "main" {
		t.Fatalf("ref = %q, want main", ref)
	}
}
