package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeBin drops a shell script named `name` on a temp dir prepended to PATH.
func writeFakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-bin probe test is POSIX-only")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProbeAgentWork_BranchAndGithubPR(t *testing.T) {
	bin := t.TempDir()
	// git: respond to the two reads the probe makes.
	writeFakeBin(t, bin, "git", `
case "$*" in
  *"rev-parse --abbrev-ref HEAD"*) echo "feature/login" ;;
  *"rev-parse --git-dir"*)         echo ".git" ;;
  *"status --porcelain"*)          echo " M file.go" ;;
  *) echo "" ;;
esac`)
	// gh: emit the JSON the probe asks for.
	writeFakeBin(t, bin, "gh", `
echo '{"number":42,"state":"OPEN","isDraft":true,"url":"https://x/pull/42","statusCheckRollup":[{"conclusion":"SUCCESS"}]}'`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	repo := t.TempDir()
	aw := ProbeAgentWork(repo)
	if aw == nil {
		t.Fatal("expected agent work, got nil")
	}
	if aw.Branch != "feature/login" {
		t.Errorf("branch = %q", aw.Branch)
	}
	if !aw.Dirty {
		t.Error("expected dirty tree")
	}
	if aw.PR == nil {
		t.Fatal("expected a PR")
	}
	if aw.PR.State != "open" || !aw.PR.Draft || aw.PR.Number != 42 {
		t.Errorf("pr = %+v", aw.PR)
	}
	if aw.PR.CI != "passing" {
		t.Errorf("ci = %q, want passing", aw.PR.CI)
	}
}

func TestProbeAgentWork_NoGitRepo(t *testing.T) {
	bin := t.TempDir()
	writeFakeBin(t, bin, "git", `exit 128`) // not a repo
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if aw := ProbeAgentWork(t.TempDir()); aw != nil {
		t.Errorf("expected nil for non-repo, got %+v", aw)
	}
}

func TestNormalizeCIConclusion(t *testing.T) {
	cases := map[string]string{
		"SUCCESS": "passing", "FAILURE": "failing", "PENDING": "pending",
		"": "none", "weird": "pending",
	}
	for in, want := range cases {
		if got := normalizeCIConclusion(in); got != want {
			t.Errorf("normalizeCIConclusion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasUncommittedWorkCountsUntrackedFiles(t *testing.T) {
	// The difference from isTreeDirty, and the reason this exists: a session's
	// newly created files are the work most easily lost, and `git diff` alone
	// says nothing about them.
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))

	if HasUncommittedWork(repo) {
		t.Fatal("a fresh repo must be reported clean")
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if !HasUncommittedWork(repo) {
		t.Error("an untracked file is uncommitted work a fresh session would not find")
	}
}

func TestHasUncommittedWorkRespectsGitignore(t *testing.T) {
	// Otherwise every stack with build output reports every repo as dirty, and
	// the count in a handover note stops meaning anything.
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	commitAll(t, repo, "ignore build")

	if err := os.MkdirAll(filepath.Join(repo, "build"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "build", "out.js"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if HasUncommittedWork(repo) {
		t.Error("ignored build output must not count as uncommitted work")
	}
}

func TestHasUncommittedWorkOnANonRepositoryIsFalse(t *testing.T) {
	if HasUncommittedWork(t.TempDir()) {
		t.Error("a directory that is not a git repo has no uncommitted work")
	}
}

func TestProbeRepoStateReadsBranchAndUncommittedWork(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))

	got, ok := ProbeRepoState(repo)
	if !ok {
		t.Fatal("ProbeRepoState() reported no repository for a git checkout")
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q, want main", got.Branch)
	}
	if got.Dirty {
		t.Error("a freshly committed repo must not be reported dirty")
	}

	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got, _ := ProbeRepoState(repo); !got.Dirty {
		t.Error("an untracked file is uncommitted work a fresh session would not find")
	}
}

func TestProbeRepoStateReportsNoBranchWhenDetached(t *testing.T) {
	// git prints the literal "HEAD" for a detached checkout. Passing that
	// through would put "HEAD" in a handover note as though it were a branch
	// name someone could check out again.
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	gitIn(t, repo, "checkout", "-q", "--detach")

	got, ok := ProbeRepoState(repo)
	if !ok {
		t.Fatal("ProbeRepoState() reported no repository for a detached checkout")
	}
	if got.Branch != "" {
		t.Errorf("branch = %q, want empty for a detached HEAD", got.Branch)
	}
}

func TestProbeRepoStateOnANonRepository(t *testing.T) {
	if _, ok := ProbeRepoState(t.TempDir()); ok {
		t.Error("a plain directory is not a repository")
	}
	if _, ok := ProbeRepoState(""); ok {
		t.Error("an empty path is not a repository")
	}
}

func TestProbeRepoStateMakesNoForgeCalls(t *testing.T) {
	// The point of this function existing alongside ProbeAgentWork: it is used
	// on the restart path, where a session that died because the network went
	// away must not then block on `gh pr view` once per repository.
	dir := t.TempDir()
	repo := newRepo(t, filepath.Join(dir, "api"))

	// A gh that fails the test if it is ever run, ahead of any real one.
	fakeBin := t.TempDir()
	writeFakeBin(t, fakeBin, "gh", "#!/bin/sh\necho CALLED > "+filepath.Join(dir, "gh-was-called")+"\nexit 0\n")
	writeFakeBin(t, fakeBin, "glab", "#!/bin/sh\necho CALLED > "+filepath.Join(dir, "glab-was-called")+"\nexit 0\n")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, ok := ProbeRepoState(repo); !ok {
		t.Fatal("ProbeRepoState() reported no repository")
	}

	for _, marker := range []string{"gh-was-called", "glab-was-called"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			t.Errorf("%s: a forge CLI was invoked on the restart path", marker)
		}
	}
}
