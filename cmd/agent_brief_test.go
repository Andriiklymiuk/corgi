package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestProbeWorktreeReposReportsBranchesNobodyRemembers(t *testing.T) {
	// A cross-repo branch is exactly what a restarted session cannot discover:
	// from a fresh session's cwd, four worktrees on one branch look like
	// nothing at all.
	dir := t.TempDir()
	base := utils.AgentWorktreeBase(dir)
	gitRepo(t, filepath.Join(base, "api@feature-referral"), "feature/referral")
	gitRepo(t, filepath.Join(base, "web@feature-referral"), "feature/referral")

	got := probeWorktreeRepos(dir)

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
	// The service name comes from the directory prefix, so the note names the
	// service rather than a flattened directory.
	services := map[string]bool{got[0].Service: true, got[1].Service: true}
	for _, want := range []string{"api", "web"} {
		if !services[want] {
			t.Errorf("service %q missing from %+v", want, got)
		}
	}
}

func TestProbeWorktreeReposReportsUncommittedWork(t *testing.T) {
	// Uncommitted work is the part that is genuinely lost if nobody mentions
	// it, because the next session has no reason to look.
	dir := t.TempDir()
	repo := gitRepo(t, filepath.Join(utils.AgentWorktreeBase(dir), "api@feature-x"), "feature/x")
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := probeWorktreeRepos(dir)

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

	if got := probeWorktreeRepos(dir); len(got) != 0 {
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
