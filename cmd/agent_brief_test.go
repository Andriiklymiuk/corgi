package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"

	"github.com/spf13/cobra"
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

func TestFormatBriefsShowsWhereTheSessionLeftOff(t *testing.T) {
	// This is the text someone reads to decide where they were, so what it
	// contains is behaviour, not decoration.
	ended := time.Date(2026, 8, 14, 14, 32, 0, 0, time.Local)
	got := formatBriefs([]brief.Brief{{
		WorkspaceID: "acme-stack",
		EndedAt:     ended,
		Cause:       "network-timeout",
		Reason:      "remote control restarted",
		Repos: []brief.RepoState{
			{Service: "api", Branch: "feature/referral", Worktree: true},
			{Service: "web", Branch: "feature/referral", Dirty: true, Worktree: true},
		},
	}})

	for _, want := range []string{
		"acme-stack",
		"network-timeout",
		"remote control restarted",
		"2026-08-14 14:32",
		"feature/referral",
		"uncommitted changes",
		"(worktree)",
		"api",
		"web",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q:\n%s", want, got)
		}
	}
}

func TestFormatBriefsOmitsWhatItDoesNotKnow(t *testing.T) {
	// Empty lines labelled "reason" and "state" would suggest corgi looked and
	// found nothing, rather than that there was nothing to look at.
	got := formatBriefs([]brief.Brief{{
		WorkspaceID: "acme",
		EndedAt:     time.Now(),
		Cause:       "crash",
	}})

	if strings.Contains(got, "reason") {
		t.Errorf("a brief with no reason must not print the label:\n%s", got)
	}
	if strings.Contains(got, "state") {
		t.Errorf("a brief with nothing to summarize must not print the label:\n%s", got)
	}
}

func TestFormatBriefsMarksADetachedCheckout(t *testing.T) {
	// An empty branch renders as a dash, never as a blank column that reads
	// like the value was lost.
	got := formatBriefs([]brief.Brief{{
		WorkspaceID: "acme",
		EndedAt:     time.Now(),
		Repos:       []brief.RepoState{{Service: "api"}},
	}})

	if !strings.Contains(got, "-") {
		t.Errorf("a repo with no branch should render a dash:\n%s", got)
	}
}

func TestFormatBriefsSaysSoWhenThereAreNone(t *testing.T) {
	got := formatBriefs(nil)

	if !strings.Contains(got, "nothing has restarted") {
		t.Errorf("empty output = %q, want it to explain the absence", got)
	}
}

func TestAgentBriefJSONShapes(t *testing.T) {
	// docs/agents.md promises an array for the list form and one object or null
	// for a single id. A command that switches shapes makes every consumer
	// branch on the shape before it can read the data.
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)

	agentPath, err := agentDir()
	if err != nil {
		t.Fatalf("agentDir() error = %v", err)
	}
	if err := brief.Write(agentPath, brief.Capture(brief.Params{
		WorkspaceID: "acme",
		Cause:       "network-timeout",
	}, []brief.RepoState{{Service: "api", Branch: "feature/referral"}})); err != nil {
		t.Fatalf("brief.Write() error = %v", err)
	}

	if out := captureStdout(t, func() { runAgentBrief(briefCmdWithJSON(t), nil) }); !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("list form = %s, want a JSON array", out)
	}
	if out := captureStdout(t, func() { runAgentBrief(briefCmdWithJSON(t), []string{"acme"}) }); !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("single form = %s, want one JSON object", out)
	}
	if out := captureStdout(t, func() { runAgentBrief(briefCmdWithJSON(t), []string{"nonesuch"}) }); strings.TrimSpace(out) != "null" {
		t.Errorf("missing brief = %s, want null", out)
	}
}

// briefCmdWithJSON returns the brief command with --json set, so the handler
// can be driven without going through the root command.
func briefCmdWithJSON(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.Flags().Bool("json", true, "")
	return c
}

func TestProbeWorkspaceReposNamesServicesFromTheComposeFile(t *testing.T) {
	// End to end over the bug this file's naming logic exists for: worktree
	// directories are named from the git repository ROOT, so a map keyed on the
	// service path silently never matches and every worktree gets labelled with
	// the repository basename instead of its service name.
	//
	// The service therefore lives in a SUBDIRECTORY of its repository — a
	// monorepo, which is exactly the layout where the two paths diverge. With
	// the service path as the key this test reports "mono" instead of "api".
	dir := t.TempDir()
	repo := gitRepo(t, filepath.Join(dir, "mono"), "main")
	servicePath := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(servicePath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	compose := "services:\n  api:\n    path: ./mono/services/api\n"
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	root, ok := utils.RepoRootOf(servicePath)
	if !ok {
		t.Fatalf("RepoRootOf(%s) reported no repository", servicePath)
	}
	if root == servicePath {
		t.Fatal("the service path and its repository root must differ for this test to discriminate")
	}
	// Named from the repository root, the way corgi names it.
	worktreeDir(t, dir, root, "feature/referral")

	got := probeWorkspaceRepos(dir)

	var worktrees int
	for _, r := range got {
		if r.Service != "api" {
			t.Errorf("service = %q, want api — the compose file names it", r.Service)
		}
		if r.Worktree {
			worktrees++
			if r.Branch != "feature/referral" {
				t.Errorf("worktree branch = %q, want feature/referral", r.Branch)
			}
		}
	}
	if worktrees != 1 {
		t.Errorf("probed %+v, want exactly one worktree", got)
	}
}
