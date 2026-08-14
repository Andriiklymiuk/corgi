package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo makes a git repository with one commit on main.
func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeRepoFile(t, filepath.Join(dir, "README.md"), "initial\n")
	commitAll(t, dir, "init")
	return dir
}

func writeRepoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiffStackCountsAcrossRepos(t *testing.T) {
	base := t.TempDir()
	api := newRepo(t, filepath.Join(base, "api"))
	web := newRepo(t, filepath.Join(base, "web"))

	for _, repo := range []string{api, web} {
		gitIn(t, repo, "checkout", "-q", "-b", "feature/x")
		writeRepoFile(t, filepath.Join(repo, "README.md"), "initial\nchanged\n")
		commitAll(t, repo, "change")
	}

	got := DiffStack(map[string]string{"api": api, "web": web}, "main", true)

	if got.Additions != 2 {
		t.Errorf("additions = %d, want 2 (one per repo)", got.Additions)
	}
	if len(got.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(got.Repos))
	}
	// Sorted for stable output across calls.
	if got.Repos[0].Service != "api" || got.Repos[1].Service != "web" {
		t.Errorf("repos should be sorted by service, got %s then %s", got.Repos[0].Service, got.Repos[1].Service)
	}
	if got.Repos[0].Branch != "feature/x" {
		t.Errorf("branch = %q, want feature/x", got.Repos[0].Branch)
	}
}

// An agent's first act is usually to create a file, and `git diff` shows
// nothing for an untracked one — so this is the case that matters most.
func TestDiffIncludesUntrackedFiles(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "web"))
	writeRepoFile(t, filepath.Join(repo, "Signup.tsx"), "line one\nline two\n")

	got := DiffStack(map[string]string{"web": repo}, "main", true)

	files := got.Repos[0].Files
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1 — a newly created file must not read as an empty diff", len(files))
	}
	if !files[0].New {
		t.Error("a new file should be flagged as such")
	}
	if files[0].Additions != 2 {
		t.Errorf("additions = %d, want 2", files[0].Additions)
	}
	if !strings.Contains(files[0].Patch, "+line one") {
		t.Errorf("patch should render like any other diff, got %q", files[0].Patch)
	}
	if got.Additions != 2 {
		t.Errorf("stack additions = %d, want 2", got.Additions)
	}
}

func TestDiffRespectsGitignore(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	writeRepoFile(t, filepath.Join(repo, ".gitignore"), "secrets.env\n")
	commitAll(t, repo, "ignore secrets")
	writeRepoFile(t, filepath.Join(repo, "secrets.env"), "TOKEN=super-secret\n")

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	for _, f := range got.Repos[0].Files {
		if f.Path == "secrets.env" {
			t.Fatal("an ignored file must not appear in the diff; that is how a token ends up in a transcript")
		}
	}
	if strings.Contains(marshalPatches(got), "super-secret") {
		t.Fatal("ignored file contents leaked into the diff")
	}
}

func marshalPatches(s *StackDiff) string {
	var b strings.Builder
	for _, r := range s.Repos {
		for _, f := range r.Files {
			b.WriteString(f.Patch)
		}
	}
	return b.String()
}

func TestDiffFilesIsNeverNil(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	if got.Repos[0].Files == nil {
		t.Error("files must marshal as [] not null, or a client that iterates it crashes")
	}
}

func TestDiffReportsNonRepoWithoutFailingTheRest(t *testing.T) {
	base := t.TempDir()
	api := newRepo(t, filepath.Join(base, "api"))
	notRepo := filepath.Join(base, "plain")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}

	got := DiffStack(map[string]string{"api": api, "plain": notRepo}, "main", true)

	if len(got.Repos) != 2 {
		t.Fatalf("both services should be reported, got %d", len(got.Repos))
	}
	for _, r := range got.Repos {
		if r.Service == "plain" && r.Error == "" {
			t.Error("a non-repository should report an error rather than being dropped silently")
		}
	}
}

func TestDiffReportsMissingBase(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))

	got := DiffStack(map[string]string{"api": repo}, "no-such-branch", true)

	if got.Repos[0].Error == "" {
		t.Error("an unknown base branch must be reported, not silently produce an empty diff")
	}
}

func TestDiffWithoutPatchesStillCounts(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	writeRepoFile(t, filepath.Join(repo, "new.txt"), "a\nb\n")

	got := DiffStack(map[string]string{"api": repo}, "main", false)

	if got.Additions != 2 {
		t.Errorf("additions = %d, want 2", got.Additions)
	}
	if got.Repos[0].Files[0].Patch != "" {
		t.Error("includePatch=false should omit the patch body")
	}
}

func TestLargePatchIsTruncatedNotDropped(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	writeRepoFile(t, filepath.Join(repo, "huge.txt"), strings.Repeat("a line of text\n", 8000))

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	f := got.Repos[0].Files[0]
	if !f.Truncated {
		t.Fatal("a very large patch must be truncated; one generated lockfile would otherwise break the whole response")
	}
	if f.Patch == "" {
		t.Error("truncated must still carry the beginning of the patch, not drop it")
	}
	if f.Additions != 8000 {
		t.Errorf("additions = %d, want the real count even when the patch is truncated", f.Additions)
	}
}

func TestBinaryFileIsFlaggedNotInlined(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	if err := os.WriteFile(filepath.Join(repo, "logo.png"), []byte{0x89, 0x50, 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	f := got.Repos[0].Files[0]
	if !f.Binary {
		t.Error("a binary file should be flagged rather than rendered as text")
	}
	if f.Patch != "" {
		t.Error("binary content must not be inlined into the response")
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one\n", 1},
		{"one", 1},
		{"one\ntwo\n", 2},
		{"one\ntwo", 2},
	}
	for _, tt := range tests {
		if got := countLines([]byte(tt.in)); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestServiceDirsPrefersWorktrees(t *testing.T) {
	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "api", AbsolutePath: "/checkouts/api"},
		{ServiceName: "web", AbsolutePath: "/checkouts/web"},
	}}
	set := &WorktreeSet{Worktrees: []RepoWorktree{{Service: "api", Dir: "/worktrees/api"}}}

	dirs := ServiceDirs(corgi, set)

	if dirs["api"] != "/worktrees/api" {
		t.Errorf("api = %q; the agent's worktree must win over the user's own checkout", dirs["api"])
	}
	if dirs["web"] != "/checkouts/web" {
		t.Errorf("web = %q; a service with no worktree keeps its checkout", dirs["web"])
	}
}

func TestServiceDirsWithoutWorktrees(t *testing.T) {
	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: "/checkouts/api"}}}

	if dirs := ServiceDirs(corgi, nil); dirs["api"] != "/checkouts/api" {
		t.Errorf("api = %q", dirs["api"])
	}
}

// Two services in different subdirectories of one repository are still one
// repository. Keying the de-duplication on the raw service directory diffed it
// twice and doubled the stack totals.
func TestDiffCountsAServiceSharedRepoOnce(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "monorepo"))
	writeRepoFile(t, filepath.Join(repo, "packages", "api", "main.go"), "package main\n")
	writeRepoFile(t, filepath.Join(repo, "packages", "web", "app.tsx"), "export {}\n")

	got := DiffStack(map[string]string{
		"api": filepath.Join(repo, "packages", "api"),
		"web": filepath.Join(repo, "packages", "web"),
	}, "main", true)

	if len(got.Repos) != 1 {
		t.Fatalf("repos = %d, want 1 — both services live in one repository", len(got.Repos))
	}
	if len(got.Repos[0].AlsoServing) != 1 {
		t.Errorf("alsoServing = %v, want the second service named", got.Repos[0].AlsoServing)
	}
	if got.Additions != got.Repos[0].Additions {
		t.Errorf("stack additions %d != repo additions %d; the repo was counted twice",
			got.Additions, got.Repos[0].Additions)
	}
}

// `git diff --numstat` reports a rename as the single path "old => new", which
// is neither a usable display name nor a pathspec that matches anything — every
// renamed file came back with an empty patch.
func TestDiffHandlesRenamedFiles(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	writeRepoFile(t, filepath.Join(repo, "old-name.go"), "package main\n\nfunc main() {}\n")
	commitAll(t, repo, "add file")
	gitIn(t, repo, "checkout", "-q", "-b", "feature/rename")
	gitIn(t, repo, "mv", "old-name.go", "new-name.go")
	commitAll(t, repo, "rename it")

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	if len(got.Repos[0].Files) != 1 {
		t.Fatalf("files = %+v, want 1", got.Repos[0].Files)
	}
	f := got.Repos[0].Files[0]
	if strings.Contains(f.Path, "=>") {
		t.Errorf("path = %q; the raw numstat rename form is not a usable path", f.Path)
	}
	if f.Path != "new-name.go" {
		t.Errorf("path = %q, want the new name", f.Path)
	}
	if f.RenamedFrom != "old-name.go" {
		t.Errorf("renamedFrom = %q, want the old name", f.RenamedFrom)
	}
}

func TestWorktreeDirsIncludesOnlyMaterializedServices(t *testing.T) {
	set := &WorktreeSet{Worktrees: []RepoWorktree{
		{Service: "api", Dir: "/wt/api"},
		{Service: "web", Skipped: "not a git repository"},
	}}

	dirs := WorktreeDirs(set)

	if len(dirs) != 1 || dirs["api"] != "/wt/api" {
		t.Errorf("dirs = %v; only services with a worktree belong in a branch diff", dirs)
	}
	if WorktreeDirs(nil) == nil {
		t.Error("a nil set should still yield a usable empty map")
	}
}

// git C-quotes paths with non-ASCII characters unless -z is used, and the
// literal quoted name cannot be opened — so the file was silently dropped from
// precisely the set an agent's work consists of: newly created files.
func TestDiffIncludesUntrackedFilesWithAwkwardNames(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "web"))
	for _, name := range []string{"café.ts", "a file with spaces.md", "naïve.txt"} {
		writeRepoFile(t, filepath.Join(repo, name), "one\ntwo\n")
	}

	got := DiffStack(map[string]string{"web": repo}, "main", true)

	if len(got.Repos[0].Files) != 3 {
		var names []string
		for _, f := range got.Repos[0].Files {
			names = append(names, f.Path)
		}
		t.Fatalf("files = %v, want all three", names)
	}
	for _, f := range got.Repos[0].Files {
		if strings.HasPrefix(f.Path, `"`) {
			t.Errorf("path %q is still C-quoted", f.Path)
		}
		if f.Additions != 2 {
			t.Errorf("%s: additions = %d, want 2", f.Path, f.Additions)
		}
	}
}

// The per-file cap does not bound the response: many modest files still add up
// to a payload a phone client cannot take.
func TestDiffBudgetsTheWholeResponse(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "api"))
	body := strings.Repeat("a line of text\n", 4000) // ~56KB each
	for i := range 40 {
		writeRepoFile(t, filepath.Join(repo, fmt.Sprintf("file%02d.txt", i)), body)
	}

	got := DiffStack(map[string]string{"api": repo}, "main", true)

	total := 0
	for _, f := range got.Repos[0].Files {
		total += len(f.Patch)
	}
	if total > maxStackPatchBytes+maxPatchBytes {
		t.Errorf("total patch bytes = %d, want the response bounded near %d", total, maxStackPatchBytes)
	}
	if !got.PatchesTruncated {
		t.Error("the response should say that some patch bodies were dropped")
	}
	// Counts survive even where the body was dropped, so the shape is complete.
	for _, f := range got.Repos[0].Files {
		if f.Additions == 0 {
			t.Errorf("%s lost its line count", f.Path)
		}
	}
}
