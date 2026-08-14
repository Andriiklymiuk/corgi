package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestWorktreeDirNameDistinguishesSameNamedRepos(t *testing.T) {
	// A stack can hold ~/work/api and ~/oss/api. Keying on the basename alone
	// sent both to one destination, and the second service was silently pointed
	// at the first repository's worktree.
	a := worktreeDirName("/home/dev/work/api", "feature/x")
	b := worktreeDirName("/home/dev/oss/api", "feature/x")

	if a == b {
		t.Fatalf("two repos named api collided on %q", a)
	}
	if !strings.HasPrefix(a, "api-") || !strings.Contains(a, "@feature-x") {
		t.Errorf("name %q should stay readable: repo, disambiguator, branch", a)
	}
	if a != worktreeDirName("/home/dev/work/api", "feature/x") {
		t.Error("the name must be stable for the same repo and branch")
	}
}

func TestMaterializeKeepsSameNamedReposApart(t *testing.T) {
	dir := t.TempDir()
	one := newRepo(t, filepath.Join(dir, "work", "api"))
	two := newRepo(t, filepath.Join(dir, "oss", "api"))
	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "work-api", AbsolutePath: one},
		{ServiceName: "oss-api", AbsolutePath: two},
	}}

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	if set.Worktrees[0].Dir == set.Worktrees[1].Dir {
		t.Fatal("two different repositories must not share a worktree directory")
	}
	for _, w := range set.Worktrees {
		root, _ := gitOut(w.Dir, gitRevParse, "--path-format=absolute", "--git-common-dir")
		if !strings.HasPrefix(root, w.Repo) {
			t.Errorf("%s points at %s, expected a worktree of %s", w.Service, root, w.Repo)
		}
	}
}

func TestReleaseDoesNotTouchASimilarlyNamedBranch(t *testing.T) {
	corgi, dir := stack(t, "api")

	slashed, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/login", nil)
	if err != nil {
		t.Fatal(err)
	}

	// feature-login flattens to the same directory segment as feature/login.
	removed, err := ReleaseBranchWorktrees(dir, "feature-login")
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Errorf("removed %v; a different branch that merely flattens the same must be left alone", removed)
	}
	if _, statErr := os.Stat(slashed.Worktrees[0].Dir); statErr != nil {
		t.Error("feature/login's worktree was force-removed by releasing feature-login")
	}
}

func TestExistingBranchWorktreesCreatesNothing(t *testing.T) {
	corgi, dir := stack(t, "api")

	set, err := ExistingBranchWorktrees(corgi, dir, "feature/never-made")
	if err != nil {
		t.Fatal(err)
	}

	if len(set.Worktrees) != 0 {
		t.Error("a read-only lookup must not report worktrees that do not exist")
	}
	if _, statErr := os.Stat(AgentWorktreeBase(dir)); !os.IsNotExist(statErr) {
		t.Error("a read-only lookup must not create the worktree directory either")
	}
	if local, _ := branchIsKnown(corgi.Services[0].AbsolutePath, "feature/never-made"); local {
		t.Error("a read-only lookup must not create the branch")
	}
}

func TestExistingBranchWorktreesFindsMaterializedOnes(t *testing.T) {
	corgi, dir := stack(t, "api", "web")
	made, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	found, err := ExistingBranchWorktrees(corgi, dir, "feature/x")
	if err != nil {
		t.Fatal(err)
	}

	if len(found.Worktrees) != len(made.Worktrees) {
		t.Fatalf("found %d worktrees, want %d", len(found.Worktrees), len(made.Worktrees))
	}
	for i := range found.Worktrees {
		if found.Worktrees[i].Dir != made.Worktrees[i].Dir {
			t.Errorf("%s: found %q, made %q", found.Worktrees[i].Service, found.Worktrees[i].Dir, made.Worktrees[i].Dir)
		}
	}
}

func TestMaterializePreparesRepositoriesConcurrently(t *testing.T) {
	// Each repository may consult origin, and this runs inside an MCP handler
	// holding a process-wide lock. The cost must be the slowest repository, not
	// the sum of them, or a stack with unreachable remotes freezes the server.
	corgi, dir := stack(t, "api", "web", "mobile", "docs", "worker")

	start := time.Now()
	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if len(set.Worktrees) != 5 {
		t.Fatalf("worktrees = %d, want 5", len(set.Worktrees))
	}
	// Generous, but a serial implementation over five repos with any remote
	// probing at all would not come close.
	if elapsed > 30*time.Second {
		t.Errorf("took %v for 5 repos; preparation should overlap", elapsed)
	}
}

func TestMaterializeStillSharesAWorktreeWhenPreparedConcurrently(t *testing.T) {
	dir := t.TempDir()
	repo := newRepo(t, filepath.Join(dir, "monorepo"))
	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "api", AbsolutePath: repo},
		{ServiceName: "worker", AbsolutePath: repo},
		{ServiceName: "cron", AbsolutePath: repo},
	}}

	set, err := MaterializeBranchAcrossRepos(corgi, dir, "feature/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(set.Worktrees) != 3 {
		t.Fatalf("worktrees = %d, want one entry per service", len(set.Worktrees))
	}
	for _, w := range set.Worktrees[1:] {
		if w.Dir != set.Worktrees[0].Dir {
			t.Errorf("%s got %q, want the shared %q", w.Service, w.Dir, set.Worktrees[0].Dir)
		}
	}
}
