package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stack builds a compose directory with n git repos wired as services.
func stack(t *testing.T, services ...string) (*CorgiCompose, string) {
	t.Helper()
	dir := t.TempDir()
	corgi := &CorgiCompose{}
	for _, name := range services {
		repo := newRepo(t, filepath.Join(dir, name))
		corgi.Services = append(corgi.Services, Service{ServiceName: name, AbsolutePath: repo})
	}
	return corgi, dir
}

func TestMaterializeCreatesOneBranchAcrossEveryRepo(t *testing.T) {
	corgi, dir := stack(t, "api", "web", "mobile")

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/referral", nil)
	if err != nil {
		t.Fatalf("MaterializeBranchAcrossRepos() = %v", err)
	}

	if len(set.Worktrees) != 3 {
		t.Fatalf("worktrees = %d, want one per service", len(set.Worktrees))
	}
	for _, w := range set.Worktrees {
		if w.Skipped != "" {
			t.Errorf("%s skipped: %s", w.Service, w.Skipped)
			continue
		}
		if !w.Created {
			t.Errorf("%s: branch should have been created", w.Service)
		}
		if _, err := os.Stat(w.Dir); err != nil {
			t.Errorf("%s: worktree dir missing: %v", w.Service, err)
		}
		// This is the whole point: one branch name, every repository.
		if got, _ := gitOut(w.Dir, gitRevParse, gitAbbrevRef, "HEAD"); got != "feature/referral" {
			t.Errorf("%s is on %q, want feature/referral", w.Service, got)
		}
	}
}

func TestMaterializeOnlyNamedServices(t *testing.T) {
	corgi, dir := stack(t, "api", "web", "mobile")

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", []string{"api", "mobile"})
	if err != nil {
		t.Fatal(err)
	}

	if len(set.Worktrees) != 2 {
		t.Fatalf("worktrees = %d, want 2", len(set.Worktrees))
	}
	for _, w := range set.Worktrees {
		if w.Service == "web" {
			t.Error("web was not asked for and must not be touched")
		}
	}
}

func TestMaterializeSharesOneWorktreeForRepoUsedTwice(t *testing.T) {
	dir := t.TempDir()
	repo := newRepo(t, filepath.Join(dir, "monorepo"))
	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "api", AbsolutePath: repo},
		{ServiceName: "worker", AbsolutePath: repo},
	}}

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	if set.Worktrees[0].Dir != set.Worktrees[1].Dir {
		t.Error("two services in one repository must share a worktree — git allows a branch in exactly one")
	}
}

func TestMaterializeIsIdempotent(t *testing.T) {
	corgi, dir := stack(t, "api")

	first, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatalf("re-materializing must not fail: %v", err)
	}

	if first.Worktrees[0].Dir != second.Worktrees[0].Dir {
		t.Error("the same branch should resolve to the same worktree on a second call")
	}
	if second.Worktrees[0].Created {
		t.Error("the second call should reuse the branch, not report it as created")
	}
}

func TestMaterializePreservesUncommittedWork(t *testing.T) {
	corgi, dir := stack(t, "api")

	set, _ := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	scratch := filepath.Join(set.Worktrees[0].Dir, "work-in-progress.txt")
	writeRepoFile(t, scratch, "half-written\n")

	if _, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(scratch); err != nil {
		t.Error("re-materializing must not throw away uncommitted work in the worktree")
	}
}

func TestMaterializeReportsNonRepoWithoutFailingTheRest(t *testing.T) {
	corgi, dir := stack(t, "api")
	plain := filepath.Join(dir, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	corgi.Services = append(corgi.Services, Service{ServiceName: "plain", AbsolutePath: plain})

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatalf("one bad service must not fail the whole call: %v", err)
	}

	var sawSkip bool
	for _, w := range set.Worktrees {
		if w.Service == "plain" {
			sawSkip = w.Skipped != ""
		}
	}
	if !sawSkip {
		t.Error("a non-repository should be reported as skipped, with the reason")
	}
}

func TestMaterializeAddsItsOwnGitignoreEntry(t *testing.T) {
	corgi, dir := stack(t, "api")

	if _, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil); err != nil {
		t.Fatal(err)
	}

	// corgi_services/ is not wholly ignored, so anything new under it must add
	// its own entry or it shows up as untracked in the user's repo.
	data, err := os.ReadFile(filepath.Join(dir, "corgi_services", ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore written: %v", err)
	}
	if !strings.Contains(string(data), ".worktrees/") {
		t.Errorf(".gitignore = %q, want a .worktrees/ entry", data)
	}
}

func TestMaterializeRejectsUnsafeBranchNames(t *testing.T) {
	corgi, dir := stack(t, "api")

	for _, branch := range []string{"", "  ", "-rf", "../escape", "a..b", "has space", "with~tilde", "ends/"} {
		if _, err := MaterializeBranchAcrossRepos(corgi, dir, branch, nil); err == nil {
			t.Errorf("branch %q should be rejected", branch)
		}
	}
}

func TestReleaseRemovesOnlyThatBranchesWorktrees(t *testing.T) {
	corgi, dir := stack(t, "api", "web")

	keep, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/keep", nil)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/drop", nil)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := ReleaseBranchWorktrees(dir, "feature/drop")
	if err != nil {
		t.Fatalf("ReleaseBranchWorktrees() = %v", err)
	}

	if len(removed) != 2 {
		t.Errorf("removed %d worktrees, want 2", len(removed))
	}
	for _, w := range drop.Worktrees {
		if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", w.Dir)
		}
	}
	for _, w := range keep.Worktrees {
		if _, err := os.Stat(w.Dir); err != nil {
			t.Errorf("%s belongs to another branch and must survive", w.Dir)
		}
	}
}

func TestReleaseKeepsTheBranchItself(t *testing.T) {
	corgi, dir := stack(t, "api")
	set, _ := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	repo := set.Worktrees[0].Repo

	if _, err := ReleaseBranchWorktrees(dir, "feature/x"); err != nil {
		t.Fatal(err)
	}

	// The commits are usually the point; only the checkout is disposable.
	if local, _ := branchIsKnown(repo, "feature/x"); !local {
		t.Error("releasing a worktree must not delete the branch")
	}
}

func TestReleaseOnAnUntouchedStackIsHarmless(t *testing.T) {
	removed, err := ReleaseBranchWorktrees(t.TempDir(), "feature/never-made")

	if err != nil {
		t.Fatalf("releasing nothing must not error, got %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
}

func TestBranchDirSegmentFlattensSlashes(t *testing.T) {
	if got := branchDirSegment("feature/referral-code"); strings.Contains(got, "/") {
		t.Errorf("branchDirSegment = %q, want one path segment", got)
	}
}

func TestValidateBranchName(t *testing.T) {
	valid := []string{"main", "feature/x", "release-1.2", "user/name/thing"}
	for _, b := range valid {
		if err := validateBranchName(b); err != nil {
			t.Errorf("validateBranchName(%q) = %v, want nil", b, err)
		}
	}
	invalid := []string{"", "-x", "a..b", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`, "/leading", "trailing/"}
	for _, b := range invalid {
		if err := validateBranchName(b); err == nil {
			t.Errorf("validateBranchName(%q) = nil, want an error", b)
		}
	}
}
