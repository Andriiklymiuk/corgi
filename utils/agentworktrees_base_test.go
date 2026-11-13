package utils

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cloneInto(t *testing.T, origin, dest string) string {
	t.Helper()
	if out, err := exec.Command("git", "clone", "-q", origin, dest).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"config", "user.email", "test@example.com"}, {"config", "user.name", "test"}} {
		if out, err := exec.Command("git", append([]string{"-C", dest}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dest
}

func TestMaterializeFromBaseStartsAtFetchedOriginNotLocalHead(t *testing.T) {
	dir := t.TempDir()
	origin := newRepo(t, filepath.Join(dir, "origin-api"))
	api := cloneInto(t, origin, filepath.Join(dir, "api"))

	writeRepoFile(t, filepath.Join(origin, "remote.txt"), "landed after clone\n")
	commitAll(t, origin, "remote work")
	remoteHead, _ := gitOut(origin, gitRevParse, "HEAD")

	writeRepoFile(t, filepath.Join(api, "local.txt"), "never pushed\n")
	commitAll(t, api, "local only")

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: api}}}
	set, err := MaterializeBranchFromBase(corgi, dir, "feature/ABC-1/x", "main", []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	w := set.Worktrees[0]
	if w.Skipped != "" || !w.Created {
		t.Fatalf("worktree = %+v, want a new branch", w)
	}
	if !strings.HasPrefix(w.Dir, AgentWorktreeBase(dir)) {
		t.Errorf("dir %s is outside the workspace worktree base", w.Dir)
	}
	if head, _ := gitOut(w.Dir, gitRevParse, "HEAD"); head != remoteHead {
		t.Errorf("worktree starts at %s, want origin/main %s", head, remoteHead)
	}
}

func TestMaterializeFromBaseReusesAnExistingBranch(t *testing.T) {
	corgi, dir := stack(t, "api")
	if _, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/y", nil); err != nil {
		t.Fatal(err)
	}
	set, err := MaterializeBranchFromBase(corgi, dir, "feature/y", "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if w := set.Worktrees[0]; w.Skipped != "" || w.Created {
		t.Fatalf("worktree = %+v, want the existing one reused", w)
	}
}

func TestMaterializeFromBaseRejectsUnsafeBase(t *testing.T) {
	corgi, dir := stack(t, "api")
	if _, err := MaterializeBranchFromBase(corgi, dir, "feature/z", "--upload-pack=x", nil); err == nil {
		t.Fatal("want an error for a base that looks like a flag")
	}
}

func TestDetachedWorktreeIsInsideWorkspaceAndIdempotent(t *testing.T) {
	corgi, dir := stack(t, "api")
	api := corgi.Services[0].AbsolutePath
	head, _ := gitOut(api, gitRevParse, "HEAD")

	first, err := MaterializeDetachedWorktree(api, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, AgentWorktreeBase(dir)) {
		t.Errorf("dir %s is outside the workspace worktree base", first)
	}
	if got, _ := gitOut(first, gitRevParse, "HEAD"); got != head {
		t.Errorf("detached at %s, want %s", got, head)
	}
	if b, _ := gitOut(first, gitRevParse, gitAbbrevRef, "HEAD"); b != "HEAD" {
		t.Errorf("worktree is on branch %q, want detached", b)
	}
	second, err := MaterializeDetachedWorktree(api, dir, head)
	if err != nil || second != first {
		t.Fatalf("second call = %q, %v; want %q reused", second, err, first)
	}
}

func TestDetachedWorktreeRefusesUnknownOrFlagRefs(t *testing.T) {
	corgi, dir := stack(t, "api")
	api := corgi.Services[0].AbsolutePath
	for _, ref := range []string{"no-such-ref", "--output=x", ""} {
		if _, err := MaterializeDetachedWorktree(api, dir, ref); err == nil {
			t.Errorf("ref %q: want an error", ref)
		}
	}
}
