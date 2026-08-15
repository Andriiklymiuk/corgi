package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"
)

// gitRepo makes a repository with one commit, on a named branch.
func gitRepo(t *testing.T, dir, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run("add", ".")
	run("commit", "-qm", "initial")
	return dir
}

// worktreeDir creates a worktree checkout under dir using the real naming
// scheme, so these tests exercise the names corgi actually produces rather than
// a convenient invention.
func worktreeDir(t *testing.T, dir, repoPath, branch string) string {
	t.Helper()
	name := utils.WorktreeDirPrefix(repoPath) + "@" + strings.ReplaceAll(branch, "/", "-")
	return gitRepo(t, filepath.Join(utils.AgentWorktreeBase(dir), name), branch)
}

func TestProbeWorktreeReposReportsBranchesNobodyRemembers(t *testing.T) {
	// A cross-repo branch is exactly what a restarted session cannot discover:
	// from a fresh session's cwd, four worktrees on one branch look like
	// nothing at all.
	dir := t.TempDir()
	apiRepo, webRepo := "/dev/acme/api", "/dev/acme/web"
	worktreeDir(t, dir, apiRepo, "feature/referral")
	worktreeDir(t, dir, webRepo, "feature/referral")

	byPrefix := map[string]string{
		utils.WorktreeDirPrefix(apiRepo): "api",
		utils.WorktreeDirPrefix(webRepo): "web",
	}

	got := probeWorktreeRepos(dir, byPrefix)

	if len(got) != 2 {
		t.Fatalf("probed %d worktrees, want 2: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Branch != "feature/referral" {
			t.Errorf("%s: branch = %q, want feature/referral", r.Service, r.Branch)
		}
		if !r.Worktree {
			t.Errorf("%s must be marked as a worktree, not a main checkout", r.Service)
		}
	}
	// The compose file maps the directory back to the service. Splitting the
	// name on "@" would yield "api-3f2a1b", labelling every repo with a hash.
	services := map[string]bool{got[0].Service: true, got[1].Service: true}
	for _, want := range []string{"api", "web"} {
		if !services[want] {
			t.Errorf("service %q missing from %+v", want, got)
		}
	}
}

func TestProbeWorktreeReposFallsBackToTheRepoName(t *testing.T) {
	// With no readable compose file there is no service map, and a name with
	// the hash still attached would be worse than the repository's own name.
	dir := t.TempDir()
	worktreeDir(t, dir, "/dev/acme/api", "feature/referral")

	got := probeWorktreeRepos(dir, map[string]string{})

	if len(got) != 1 {
		t.Fatalf("probed %d worktrees, want 1", len(got))
	}
	if got[0].Service != "api" {
		t.Errorf("service = %q, want the hash trimmed back to api", got[0].Service)
	}
}

func TestProbeWorktreeReposReportsUncommittedWork(t *testing.T) {
	// Uncommitted work is the part that is genuinely lost if nobody mentions
	// it, because the next session has no reason to look.
	dir := t.TempDir()
	repo := worktreeDir(t, dir, "/dev/acme/api", "feature/x")
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := probeWorktreeRepos(dir, nil)

	if len(got) != 1 {
		t.Fatalf("probed %d worktrees, want 1", len(got))
	}
	if !got[0].Dirty {
		t.Error("a worktree with an untracked file must be reported as dirty")
	}
}

func TestProbeWorktreeReposIgnoresNonRepositories(t *testing.T) {
	dir := t.TempDir()
	base := utils.AgentWorktreeBase(dir)
	if err := os.MkdirAll(filepath.Join(base, "not-a-repo"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "stray.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if got := probeWorktreeRepos(dir, nil); len(got) != 0 {
		t.Errorf("probed %+v, want nothing — only git checkouts belong in a brief", got)
	}
}

func TestProbeWorkspaceReposOnAMissingDirectoryIsEmpty(t *testing.T) {
	// A workspace on an unmounted drive must produce a brief saying the session
	// restarted, not a crash inside the supervisor's restart path.
	if got := probeWorkspaceRepos(filepath.Join(t.TempDir(), "gone")); len(got) != 0 {
		t.Errorf("probed %+v, want nothing", got)
	}
	if got := probeWorkspaceRepos(""); len(got) != 0 {
		t.Errorf("probed %+v for an empty dir, want nothing", got)
	}
}

func TestCaptureWorkspaceBriefAlwaysReturnsABrief(t *testing.T) {
	// The daemon calls this on every restart. Returning nil for a workspace it
	// could not read would lose the cause and reason too, which are the parts
	// that always apply.
	got := captureWorkspaceBrief(brief.Params{
		WorkspaceID: "acme",
		Dir:         filepath.Join(t.TempDir(), "gone"),
		Cause:       "network-timeout",
		Reason:      "remote control restarted",
	})

	if got == nil {
		t.Fatal("captureWorkspaceBrief() = nil")
	}
	if got.WorkspaceID != "acme" || got.Cause != "network-timeout" {
		t.Errorf("brief = %+v, want the params carried through", got)
	}
	if !got.Empty() {
		t.Error("a brief with no readable repos must report itself empty so the notification stays one line")
	}
}
