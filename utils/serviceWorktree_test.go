package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBranchSlug(t *testing.T) {
	for in, want := range map[string]string{
		"feature/ABC-123": "feature-ABC-123",
		"main":            "main",
		"a b:c/d":         "a-b-c-d",
	} {
		if got := branchSlug(in); got != want {
			t.Errorf("branchSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorktreeDest(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/ws"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	got := worktreeDest("api", "feature/x")
	want := "/ws/corgi_services/.worktrees/api-feature-x"
	if got != want {
		t.Errorf("worktreeDest = %q, want %q", got, want)
	}
}

func TestCutServicePair(t *testing.T) {
	if n, v, err := cutServicePair("api=feature/x"); err != nil || n != "api" || v != "feature/x" {
		t.Errorf("got (%q,%q,%v)", n, v, err)
	}
	for _, bad := range []string{"api", "=x", "api="} {
		if _, _, err := cutServicePair(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestAssertNoServiceWorkdirConflict(t *testing.T) {
	mk := func(dir, branch, checkout []string) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().StringArray("service-dir", dir, "")
		c.Flags().StringArray("service-branch", branch, "")
		c.Flags().StringArray("service-checkout", checkout, "")
		return c
	}
	if err := assertNoServiceWorkdirConflict(mk([]string{"api=/x"}, []string{"web=feat"}, nil)); err != nil {
		t.Errorf("disjoint should be ok: %v", err)
	}
	if err := assertNoServiceWorkdirConflict(mk([]string{"api=/x"}, []string{"api=feat"}, nil)); err == nil {
		t.Error("api in both dir and branch should conflict")
	}
}

func TestApplyServiceWorkdirs(t *testing.T) {
	wt := t.TempDir()
	mk := func() *CorgiCompose {
		return &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: "/orig/api"}}}
	}

	c := mk()
	if err := ApplyServiceWorkdirs(c, []string{"api=" + wt}, nil, nil); err != nil {
		t.Fatalf("dir override: %v", err)
	}
	if c.Services[0].AbsolutePath != wt {
		t.Errorf("api dir = %q, want %q", c.Services[0].AbsolutePath, wt)
	}

	if err := ApplyServiceWorkdirs(mk(), []string{"api=" + wt}, []string{"api=feat"}, nil); err == nil {
		t.Error("same service in dir+branch should conflict")
	}

	c2 := mk()
	if err := ApplyServiceWorkdirs(c2, nil, nil, nil); err != nil || c2.Services[0].AbsolutePath != "/orig/api" {
		t.Error("empty input should be a no-op")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestEnsureServiceWorktreeReuseDirtyClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "init")
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dest := worktreeDest("api", "feature/x")
	dir, err := EnsureServiceWorktree(repo, "feature/x", dest)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dir != dest {
		t.Fatalf("expected worktree dest %s, got %s", dest, dir)
	}
	if !insideWorktree(dest) {
		t.Fatal("dest is not a worktree")
	}
	// reuse: second call on the existing healthy worktree must not error
	if _, err := EnsureServiceWorktree(repo, "feature/x", dest); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	if dirty, _ := isTreeDirty(repo); dirty {
		t.Fatal("clean repo reported dirty")
	}
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := isTreeDirty(repo); !dirty {
		t.Fatal("modified tracked file should be dirty")
	}

	if err := CleanCorgiWorktrees(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after clean")
	}
}

func TestEnsureServiceWorktreeMainAlreadyOnBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "init")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir, err := EnsureServiceWorktree(repo, "main", worktreeDest("api", "main"))
	if err != nil {
		t.Fatalf("main already on branch should not error: %v", err)
	}
	if dir != repo {
		t.Fatalf("expected main checkout %s, got %s", repo, dir)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
}

func TestApplyFeatureBranchOnlyWhereBranchExists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	withBranch := filepath.Join(root, "has")
	withoutBranch := filepath.Join(root, "hasnt")
	initRepo(t, withBranch)
	initRepo(t, withoutBranch)
	git(t, withBranch, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "has", AbsolutePath: withBranch},
		{ServiceName: "hasnt", AbsolutePath: withoutBranch},
	}}
	if err := ApplyFeatureBranch(corgi, "feature/x", nil, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if corgi.Services[0].AbsolutePath != worktreeDest("has", "feature/x") {
		t.Errorf("service with the branch should move to a worktree, got %s", corgi.Services[0].AbsolutePath)
	}
	if corgi.Services[0].CacheScope == "" {
		t.Error("relocated service should get a cache scope")
	}
	if corgi.Services[1].AbsolutePath != withoutBranch {
		t.Errorf("service without the branch must stay put, got %s", corgi.Services[1].AbsolutePath)
	}
	if corgi.Services[1].CacheScope != "" {
		t.Error("untouched service must keep an empty cache scope")
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestApplyFeatureBranchSkipsPinnedAndEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: repo}}}
	if err := ApplyFeatureBranch(corgi, "feature/x", map[string]bool{"api": true}, nil); err != nil {
		t.Fatalf("pinned: %v", err)
	}
	if corgi.Services[0].AbsolutePath != repo {
		t.Error("pinned service must not be moved by --feature")
	}
	if err := ApplyFeatureBranch(corgi, "", nil, nil); err != nil {
		t.Fatalf("empty feature: %v", err)
	}
	if corgi.Services[0].AbsolutePath != repo {
		t.Error("empty feature must be a no-op")
	}
}

func TestEnsureFeatureWorktreeFromRemoteOnlyBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	initRepo(t, origin)
	git(t, origin, "branch", "feature/x")

	clone := filepath.Join(root, "clone")
	if out, err := exec.Command("git", "clone", "--depth", "1", "--branch", "main", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	if local, _ := branchIsKnown(clone, "feature/x"); local {
		t.Fatal("shallow clone should not have the branch locally yet")
	}
	dest := worktreeDest("api", "feature/x")
	dir, err := EnsureFeatureWorktree(clone, "feature/x", dest)
	if err != nil {
		t.Fatalf("remote-only branch: %v", err)
	}
	if dir != dest {
		t.Fatalf("expected worktree at %s, got %s", dest, dir)
	}
	if cur, _ := gitOut(dest, "rev-parse", "--abbrev-ref", "HEAD"); cur != "feature/x" {
		t.Fatalf("worktree HEAD = %q, want feature/x", cur)
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestEnsureFeatureWorktreeUnknownBranchIsNoop(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir, err := EnsureFeatureWorktree(repo, "nope", worktreeDest("api", "nope"))
	if err != nil {
		t.Fatalf("missing branch must not error: %v", err)
	}
	if dir != "" {
		t.Fatalf("missing branch must return an empty dir, got %s", dir)
	}
	if dir, err := EnsureFeatureWorktree(filepath.Join(root, "not-a-repo"), "x", "d"); err != nil || dir != "" {
		t.Fatalf("non-repo must be a silent no-op, got (%q,%v)", dir, err)
	}
}

func TestPinnedServices(t *testing.T) {
	got := pinnedServices([]string{"api=/x"}, []string{"web=feat"}, nil)
	if !got["api"] || !got["web"] || len(got) != 2 {
		t.Errorf("pinnedServices = %v", got)
	}
}

func TestMaterializeServiceWorktreesFeatureFlag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	mk := func(feature string) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().StringArray("service-dir", nil, "")
		c.Flags().StringArray("service-branch", nil, "")
		c.Flags().StringArray("service-checkout", nil, "")
		c.Flags().String("feature", feature, "")
		return c
	}

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: repo}}}
	if err := MaterializeServiceWorktrees(mk(""), corgi); err != nil {
		t.Fatalf("no flags: %v", err)
	}
	if corgi.Services[0].AbsolutePath != repo {
		t.Fatal("no flags must be a no-op")
	}

	if err := MaterializeServiceWorktrees(mk("feature/x"), corgi); err != nil {
		t.Fatalf("feature: %v", err)
	}
	if corgi.Services[0].AbsolutePath != worktreeDest("api", "feature/x") {
		t.Errorf("--feature did not relocate the service, got %s", corgi.Services[0].AbsolutePath)
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestMaterializeServiceWorktreesFeatureAbsentFlag(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().StringArray("service-dir", nil, "")
	c.Flags().StringArray("service-branch", nil, "")
	c.Flags().StringArray("service-checkout", nil, "")
	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: "/orig"}}}
	if err := MaterializeServiceWorktrees(c, corgi); err != nil {
		t.Fatalf("a command without --feature must not error: %v", err)
	}
	if corgi.Services[0].AbsolutePath != "/orig" {
		t.Error("expected a no-op")
	}
}

func TestApplyServiceWorkdirsWithFeatureExplicitWins(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)
	git(t, repo, "branch", "feature/x")
	git(t, repo, "branch", "explicit")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: repo}}}
	if err := ApplyServiceWorkdirsWithFeature(corgi, nil, []string{"api=explicit"}, nil, "feature/x"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if corgi.Services[0].AbsolutePath != worktreeDest("api", "explicit") {
		t.Errorf("--service-branch must win over --feature, got %s", corgi.Services[0].AbsolutePath)
	}

	empty := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: repo}}}
	if err := ApplyServiceWorkdirsWithFeature(empty, nil, nil, nil, ""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty.Services[0].AbsolutePath != repo {
		t.Error("no inputs must be a no-op")
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestAddWorktreeUnknownBranchErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)

	err := addWorktree(repo, "nope", filepath.Join(root, "dest"))
	if err == nil {
		t.Fatal("expected an error for a branch that exists neither locally nor on origin")
	}
	if !strings.Contains(err.Error(), "worktree add") {
		t.Errorf("error should name the failing git call, got %v", err)
	}
}

func TestApplyFeatureBranchMainCheckoutAlreadyOnBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: repo}}}
	if err := ApplyFeatureBranch(corgi, "main", nil, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if corgi.Services[0].AbsolutePath != repo {
		t.Errorf("a checkout already on the branch must be used as-is, got %s", corgi.Services[0].AbsolutePath)
	}
	if corgi.Services[0].CacheScope != "" {
		t.Error("no relocation means no cache scope, so existing markers stay valid")
	}
}

func TestIsCorgiWorktreePath(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	if !isCorgiWorktreePath(worktreeDest("api", "feature/x")) {
		t.Error("a dest corgi generated must be recognised as its own")
	}
	for _, outside := range []string{"/", "/etc", CorgiComposePathDir, worktreesBase(), filepath.Join(worktreesBase(), "..", "..")} {
		if isCorgiWorktreePath(outside) {
			t.Errorf("%q must never be treated as a corgi worktree path", outside)
		}
	}
}

func TestApplyFeatureBranchSkipsSubdirectoryService(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "mono")
	initRepo(t, repo)
	sub := filepath.Join(repo, "packages", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{{ServiceName: "api", AbsolutePath: sub}}}
	if err := ApplyFeatureBranch(corgi, "feature/x", nil, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if corgi.Services[0].AbsolutePath != sub {
		t.Errorf("a service inside a repo subdirectory must be left alone, got %s", corgi.Services[0].AbsolutePath)
	}
}

func TestApplyFeatureBranchSharesOneWorktreePerRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepo(t, repo)
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "web", AbsolutePath: repo},
		{ServiceName: "worker", AbsolutePath: repo},
	}}
	if err := ApplyFeatureBranch(corgi, "feature/x", nil, nil); err != nil {
		t.Fatalf("two services on one repo must not fight over the branch: %v", err)
	}
	if corgi.Services[0].AbsolutePath != corgi.Services[1].AbsolutePath {
		t.Errorf("both services should share one worktree, got %s and %s",
			corgi.Services[0].AbsolutePath, corgi.Services[1].AbsolutePath)
	}
	if corgi.Services[0].AbsolutePath == repo {
		t.Error("expected a worktree, not the main checkout")
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestIsRepoRootThroughSymlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepo(t, repo)

	link := filepath.Join(root, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !isRepoRoot(link) {
		t.Error("a repo reached through a symlink is still its own root")
	}
	if !isRepoRoot(repo) {
		t.Error("the repo itself must be its own root")
	}
}

func TestIsShallowRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	initRepo(t, origin)
	if isShallowRepo(origin) {
		t.Error("a normal clone is not shallow")
	}
	clone := filepath.Join(root, "clone")
	// git ignores --depth for plain local paths; file:// makes it honour it.
	if out, err := exec.Command("git", "clone", "--depth", "1", "file://"+origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if !isShallowRepo(clone) {
		t.Error("a --depth 1 clone is shallow")
	}
}

func TestApplyFeatureBranchHonoursServiceSelection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	wanted := filepath.Join(root, "wanted")
	other := filepath.Join(root, "other")
	initRepo(t, wanted)
	initRepo(t, other)
	git(t, wanted, "branch", "feature/x")
	git(t, other, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "wanted", AbsolutePath: wanted},
		{ServiceName: "other", AbsolutePath: other},
	}}
	if err := ApplyFeatureBranch(corgi, "feature/x", nil, map[string]bool{"wanted": true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if corgi.Services[0].AbsolutePath == wanted {
		t.Error("the selected service should have been relocated")
	}
	if corgi.Services[1].AbsolutePath != other {
		t.Errorf("an unselected service must not get a worktree, got %s", corgi.Services[1].AbsolutePath)
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestSelectedServices(t *testing.T) {
	prev := ServicesItemsFromFlag
	t.Cleanup(func() { ServicesItemsFromFlag = prev })

	ServicesItemsFromFlag = nil
	if selectedServices() != nil {
		t.Error("no --services means every service")
	}
	ServicesItemsFromFlag = []string{"api", "web"}
	got := selectedServices()
	if !got["api"] || !got["web"] || len(got) != 2 {
		t.Errorf("selectedServices = %v", got)
	}
}

func TestEnsureServiceWorktreeReusesDifferentlyNamedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)
	git(t, repo, "branch", "feature/x")

	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	first, err := EnsureServiceWorktree(repo, "feature/x", worktreeDest("api", "feature/x"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// git allows a branch in only one worktree; a second dest must reuse the first
	// rather than fail with "already checked out".
	second, err := EnsureServiceWorktree(repo, "feature/x", worktreeDest("worker", "feature/x"))
	if err != nil {
		t.Fatalf("second dest for the same branch must reuse, not fail: %v", err)
	}
	firstReal, _ := realPath(first)
	secondReal, _ := realPath(second)
	if secondReal != firstReal {
		t.Errorf("expected reuse of %s, got %s", first, second)
	}
	if _, err := os.Stat(filepath.Join(second, "f.txt")); err != nil {
		t.Errorf("the reused path must be a usable checkout: %v", err)
	}
	t.Cleanup(func() { _ = CleanCorgiWorktrees() })
}

func TestWorktreeForBranchAbsent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	initRepo(t, repo)

	if got := worktreeForBranch(repo, "feature/x"); got != "" {
		t.Errorf("no worktree for an unused branch, got %q", got)
	}
	if got := worktreeForBranch(filepath.Join(root, "nope"), "main"); got != "" {
		t.Errorf("a non-repo yields nothing, got %q", got)
	}
}
