package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const remoteProbeTimeout = 30 * time.Second

// git subcommands and flags repeated across this file.
const (
	gitRevParse  = "rev-parse"
	gitAbbrevRef = "--abbrev-ref"
)

// Flag names shared by the workdir overrides.
const (
	flagServiceDir      = "service-dir"
	flagServiceBranch   = "service-branch"
	flagServiceCheckout = "service-checkout"
)

// featurePrefix labels every --feature decision in the log.
const featurePrefix = "feature:"

// git output goes to stderr so --json stdout stays pure JSON.
func gitRun(dir string, args ...string) error {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// noPromptEnv keeps a git call that reaches the network from blocking on an
// interactive credential prompt. Configured credential helpers still apply; only
// the terminal fallback is disabled, so an unreachable remote fails fast.
func noPromptEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
}

func gitRunNoPrompt(dir string, args ...string) error {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = noPromptEnv()
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}

func gitOutNoPrompt(dir string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = noPromptEnv()
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func isTreeDirty(dir string) (bool, error) {
	out, err := gitOut(dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func insideWorktree(dir string) bool {
	out, err := gitOut(dir, gitRevParse, "--is-inside-work-tree")
	return err == nil && out == "true"
}

func isGitRepo(dir string) bool {
	_, err := gitOut(dir, gitRevParse, "--git-dir")
	return err == nil
}

func isShallowRepo(dir string) bool {
	out, err := gitOut(dir, gitRevParse, "--is-shallow-repository")
	return err == nil && out == "true"
}

func branchSlug(branch string) string {
	return strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(branch)
}

// worktreeDest is deterministic per (service, branch) so re-runs reuse one dir.
func worktreeDest(service, branch string) string {
	return filepath.Join(worktreesBase(), service+"-"+branchSlug(branch))
}

func worktreesBase() string {
	return filepath.Join(CorgiComposePathDir, "corgi_services", ".worktrees")
}

// isCorgiWorktreePath guards destructive cleanup: only paths corgi itself owns
// may be removed.
func isCorgiWorktreePath(dest string) bool {
	base, err := filepath.Abs(worktreesBase())
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cutServicePair(pair string) (name, val string, err error) {
	name, val, ok := strings.Cut(pair, "=")
	if !ok || name == "" || val == "" {
		return "", "", fmt.Errorf("expects name=value, got %q", pair)
	}
	return name, val, nil
}

// EnsureServiceWorktree returns the dir to run a branch-pinned service from:
// the main checkout when it's already on that branch, else a reused/created
// worktree at dest (keeping deps and uncommitted work).
func EnsureServiceWorktree(repo, branch, dest string) (string, error) {
	if !isGitRepo(repo) {
		return "", fmt.Errorf("%s is not a git repository (run corgi init first)", repo)
	}
	if cur, _ := gitOut(repo, gitRevParse, gitAbbrevRef, "HEAD"); cur == branch {
		return repo, nil
	}
	_ = gitRun(repo, "worktree", "prune")
	if existing := worktreeForBranch(repo, branch); existing != "" {
		return preferSpelling(existing, dest), nil
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		if insideWorktree(dest) {
			cur, _ := gitOut(dest, gitRevParse, gitAbbrevRef, "HEAD")
			if cur != branch {
				if err := gitRun(dest, "checkout", branch); err != nil {
					return "", fmt.Errorf("reuse worktree %s on %s: %v", dest, branch, err)
				}
			}
			return dest, nil
		}
		if err := os.RemoveAll(dest); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := addWorktree(repo, branch, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// preferSpelling returns dest when it names the same directory as path. git
// reports symlink-resolved paths, and corgi's own spelling keeps logs and cache
// scopes consistent across runs.
func preferSpelling(path, dest string) string {
	a, okA := realPath(path)
	b, okB := realPath(dest)
	if okA && okB && a == b {
		return dest
	}
	return path
}

// worktreeForBranch returns the path of an existing worktree already holding
// branch, or "". git allows a branch in only one worktree, so reusing it is the
// only way a second service (or a differently named dest) can run that branch.
func worktreeForBranch(repo, branch string) string {
	out, err := gitOut(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	want := "branch refs/heads/" + branch
	var path string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == want && path != "":
			if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
				return path
			}
			return ""
		}
	}
	return ""
}

// addWorktree adds dest at branch, falling back to a local branch off
// origin/<branch> when only the remote-tracking ref exists.
func addWorktree(repo, branch, dest string) error {
	err := gitRun(repo, "worktree", "add", dest, branch)
	if err == nil {
		return nil
	}
	remoteRef := "refs/remotes/origin/" + branch
	if _, refErr := gitOut(repo, gitRevParse, "--verify", "--quiet", remoteRef); refErr != nil {
		return fmt.Errorf("git worktree add %s %s: %v", dest, branch, err)
	}
	if isCorgiWorktreePath(dest) {
		_ = os.RemoveAll(dest)
	}
	if err := gitRun(repo, "worktree", "add", "-b", branch, dest, remoteRef); err != nil {
		return fmt.Errorf("git worktree add -b %s %s %s: %v", branch, dest, remoteRef, err)
	}
	return nil
}

func cmdStringArray(cmd *cobra.Command, name string) []string {
	v, err := cmd.Flags().GetStringArray(name)
	if err != nil {
		return nil
	}
	return v
}

func cmdString(cmd *cobra.Command, name string) string {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return ""
	}
	return v
}

func indexServices(corgi *CorgiCompose) map[string]*Service {
	byName := map[string]*Service{}
	for i := range corgi.Services {
		byName[corgi.Services[i].ServiceName] = &corgi.Services[i]
	}
	return byName
}

func applyCheckoutPairs(byName map[string]*Service, pairs []string) error {
	for _, pair := range pairs {
		name, branch, err := cutServicePair(pair)
		if err != nil {
			return fmt.Errorf("--service-checkout %v", err)
		}
		svc, found := byName[name]
		if !found {
			return fmt.Errorf("--service-checkout: no service named %q in corgi-compose.yml", name)
		}
		if !isGitRepo(svc.AbsolutePath) {
			return fmt.Errorf("--service-checkout %s: %s is not a git repository (run corgi init first)", name, name)
		}
		dirty, derr := isTreeDirty(svc.AbsolutePath)
		if derr != nil {
			return fmt.Errorf("--service-checkout %s: %v", name, derr)
		}
		if dirty {
			return fmt.Errorf("--service-checkout %s: %s has uncommitted changes; commit/stash, or use --service-branch for an isolated worktree", name, name)
		}
		if err := gitRun(svc.AbsolutePath, "checkout", branch); err != nil {
			return fmt.Errorf("--service-checkout %s: git checkout %s: %v", name, branch, err)
		}
		Info("service-checkout:", name, "→", branch, "(in place)")
	}
	return nil
}

func applyBranchPairs(byName map[string]*Service, pairs []string) error {
	for _, pair := range pairs {
		name, branch, err := cutServicePair(pair)
		if err != nil {
			return fmt.Errorf("--service-branch %v", err)
		}
		svc, found := byName[name]
		if !found {
			return fmt.Errorf("--service-branch: no service named %q in corgi-compose.yml", name)
		}
		dest := worktreeDest(name, branch)
		dir, err := EnsureServiceWorktree(svc.AbsolutePath, branch, dest)
		if err != nil {
			return fmt.Errorf("--service-branch %s: %v", name, err)
		}
		if dir == svc.AbsolutePath {
			Info("service-branch:", name, "→", branch, "(main checkout already on branch)")
		} else {
			Info("service-branch:", name, "→", branch, "@", dir)
		}
		pointServiceAt(svc, dir)
	}
	return nil
}

// pointServiceAt moves a service's working dir, scoping its beforeStart step
// cache when the dir is not the declared checkout — a worktree's dependency dir
// is empty even when the lockfile hash matches.
func pointServiceAt(svc *Service, dir string) {
	if dir != svc.AbsolutePath {
		svc.CacheScope = CacheScopeForDir(dir)
	}
	svc.AbsolutePath = dir
}

func branchIsKnown(repo, branch string) (local, remote bool) {
	if _, err := gitOut(repo, gitRevParse, "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		local = true
	}
	if out, err := gitProbeRemote(repo, branch); err == nil && out != "" {
		remote = true
	}
	return local, remote
}

// gitProbeRemote asks origin about one branch. Bounded, because this runs once
// per service and an unreachable remote would otherwise hang the whole run.
func gitProbeRemote(repo, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteProbeTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", repo, "ls-remote", "--heads", "origin", branch)
	c.Env = noPromptEnv()
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

// realPath resolves a path for comparison. git reports symlink-resolved paths,
// so a workspace reached through a symlink (/tmp on macOS, for one) would
// otherwise never compare equal to its own repository root.
func realPath(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, true
	}
	return resolved, true
}

func repoRoot(dir string) (string, bool) {
	out, err := gitOut(dir, gitRevParse, "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return realPath(out)
}

// isRepoRoot reports whether dir is the top level of its repository. A service
// living in a subdirectory cannot be relocated by swapping in a worktree path —
// the worktree root is the repo, not the subdirectory.
func isRepoRoot(dir string) bool {
	root, ok := repoRoot(dir)
	if !ok {
		return false
	}
	self, ok := realPath(dir)
	if !ok {
		return false
	}
	return root == self
}

// CheckoutFeatureBranch switches repo to branch in place when it carries it,
// fetching from origin first. Reports whether it switched. Unlike the worktree
// path this is for freshly cloned repos, where there is no work to preserve.
func CheckoutFeatureBranch(repo, branch string) (bool, error) {
	if branch == "" || !isGitRepo(repo) {
		return false, nil
	}
	if cur, _ := gitOut(repo, gitRevParse, gitAbbrevRef, "HEAD"); cur == branch {
		return true, nil
	}
	local, remote := branchIsKnown(repo, branch)
	if !local && !remote {
		return false, nil
	}
	if !local {
		spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
		args := []string{"fetch", "--no-tags"}
		if isShallowRepo(repo) {
			args = append(args, "--depth", "1")
		}
		args = append(args, "origin", spec)
		if err := gitRunNoPrompt(repo, args...); err != nil {
			return false, fmt.Errorf("fetch origin %s: %v", branch, err)
		}
		if err := gitRun(repo, "checkout", "-B", branch, "refs/remotes/origin/"+branch); err != nil {
			return false, fmt.Errorf("checkout %s: %v", branch, err)
		}
		return true, nil
	}
	if err := gitRun(repo, "checkout", branch); err != nil {
		return false, fmt.Errorf("checkout %s: %v", branch, err)
	}
	return true, nil
}

// EnsureFeatureWorktree materializes branch for repo when it exists locally or
// on origin, fetching the remote head first. Returns an empty dir and no error
// when the repo does not carry the branch.
func EnsureFeatureWorktree(repo, branch, dest string) (string, error) {
	if !isGitRepo(repo) {
		return "", nil
	}
	local, remote := branchIsKnown(repo, branch)
	if !local && !remote {
		return "", nil
	}
	if !local {
		spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
		args := []string{"fetch", "--no-tags"}
		if isShallowRepo(repo) {
			args = append(args, "--depth", "1")
		}
		args = append(args, "origin", spec)
		if err := gitRunNoPrompt(repo, args...); err != nil {
			return "", fmt.Errorf("fetch origin %s: %v", branch, err)
		}
	}
	return EnsureServiceWorktree(repo, branch, dest)
}

// ApplyFeatureBranch points every service whose repo carries branch at a
// worktree for it, leaving the rest on their default checkout. Services pinned
// by an explicit per-service flag are skipped.
func ApplyFeatureBranch(corgi *CorgiCompose, branch string, pinned, only map[string]bool) error {
	if branch == "" {
		return nil
	}
	// Two services can share one repository, and git allows a branch in only
	// one worktree, so the first one to need it decides where it lives.
	byRoot := map[string]string{}
	for i := range corgi.Services {
		svc := &corgi.Services[i]
		if !featureAppliesTo(svc, pinned, only) {
			continue
		}
		root, _ := repoRoot(svc.AbsolutePath)
		dir, err := worktreeForRoot(byRoot, root, svc, branch)
		if err != nil {
			return err
		}
		if dir == "" {
			Info(featurePrefix, svc.ServiceName, "→ no", branch, "branch, staying on current checkout")
			continue
		}
		if dir == svc.AbsolutePath {
			Info(featurePrefix, svc.ServiceName, "→", branch, "(main checkout already on branch)")
		} else {
			Info(featurePrefix, svc.ServiceName, "→", branch, "@", dir)
		}
		pointServiceAt(svc, dir)
	}
	return nil
}

// featureAppliesTo reports whether --feature may move this service: an explicit
// per-service flag wins, a sliced run limits the set, and a service inside a
// repository subdirectory cannot be relocated by swapping in a worktree path.
func featureAppliesTo(svc *Service, pinned, only map[string]bool) bool {
	switch {
	case pinned[svc.ServiceName]:
		return false
	case only != nil && !only[svc.ServiceName]:
		return false
	case !isGitRepo(svc.AbsolutePath):
		return false
	case !isRepoRoot(svc.AbsolutePath):
		Info(featurePrefix, svc.ServiceName, "→ skipped, it lives in a subdirectory of its repository")
		return false
	}
	return true
}

func worktreeForRoot(byRoot map[string]string, root string, svc *Service, branch string) (string, error) {
	if dir, seen := byRoot[root]; seen {
		return dir, nil
	}
	dir, err := EnsureFeatureWorktree(svc.AbsolutePath, branch, worktreeDest(svc.ServiceName, branch))
	if err != nil {
		return "", fmt.Errorf("--feature %s: %v", svc.ServiceName, err)
	}
	byRoot[root] = dir
	return dir, nil
}

func pinnedServices(groups ...[]string) map[string]bool {
	pinned := map[string]bool{}
	for _, group := range groups {
		for _, pair := range group {
			if name, _, err := cutServicePair(pair); err == nil {
				pinned[name] = true
			}
		}
	}
	return pinned
}

// conflictAcross errors if a service appears in more than one of the groups.
func conflictAcross(groups map[string][]string) error {
	seen := map[string]string{}
	for _, flag := range []string{flagServiceDir, flagServiceBranch, flagServiceCheckout} {
		for _, pair := range groups[flag] {
			name, _, err := cutServicePair(pair)
			if err != nil {
				continue
			}
			if prev, ok := seen[name]; ok {
				return fmt.Errorf("service %q given to both --%s and --%s; pick one", name, prev, flag)
			}
			seen[name] = flag
		}
	}
	return nil
}

func assertNoServiceWorkdirConflict(cmd *cobra.Command) error {
	return conflictAcross(map[string][]string{
		flagServiceDir:      cmdStringArray(cmd, flagServiceDir),
		flagServiceBranch:   cmdStringArray(cmd, flagServiceBranch),
		flagServiceCheckout: cmdStringArray(cmd, flagServiceCheckout),
	})
}

// MaterializeServiceWorktrees applies the --service-branch/--service-checkout/
// --feature flags. Side-effecting (git) — call after any dry-run guard.
func MaterializeServiceWorktrees(cmd *cobra.Command, corgi *CorgiCompose) error {
	branchPairs := cmdStringArray(cmd, flagServiceBranch)
	checkoutPairs := cmdStringArray(cmd, flagServiceCheckout)
	feature := cmdString(cmd, "feature")
	if len(branchPairs) == 0 && len(checkoutPairs) == 0 && feature == "" {
		return nil
	}
	if err := assertNoServiceWorkdirConflict(cmd); err != nil {
		return err
	}
	byName := indexServices(corgi)
	if err := applyCheckoutPairs(byName, checkoutPairs); err != nil {
		return err
	}
	if err := applyBranchPairs(byName, branchPairs); err != nil {
		return err
	}
	return ApplyFeatureBranch(corgi, feature,
		pinnedServices(cmdStringArray(cmd, flagServiceDir), branchPairs, checkoutPairs),
		selectedServices())
}

// selectedServices narrows --feature to the services this run actually starts,
// so a sliced run does not probe and lay down worktrees for the whole workspace.
// nil means every service.
func selectedServices() map[string]bool {
	if len(ServicesItemsFromFlag) == 0 {
		return nil
	}
	only := map[string]bool{}
	for _, name := range ServicesItemsFromFlag {
		only[name] = true
	}
	return only
}

// ApplyServiceWorkdirs applies dir/branch/checkout overrides from name=value
// slices (e.g. the MCP server).
func ApplyServiceWorkdirs(corgi *CorgiCompose, dirPairs, branchPairs, checkoutPairs []string) error {
	return ApplyServiceWorkdirsWithFeature(corgi, dirPairs, branchPairs, checkoutPairs, "")
}

// ApplyServiceWorkdirsWithFeature is ApplyServiceWorkdirs plus a fleet-wide
// feature branch applied to every service not already pinned.
func ApplyServiceWorkdirsWithFeature(corgi *CorgiCompose, dirPairs, branchPairs, checkoutPairs []string, feature string) error {
	if len(dirPairs) == 0 && len(branchPairs) == 0 && len(checkoutPairs) == 0 && feature == "" {
		return nil
	}
	if err := conflictAcross(map[string][]string{
		flagServiceDir:      dirPairs,
		flagServiceBranch:   branchPairs,
		flagServiceCheckout: checkoutPairs,
	}); err != nil {
		return err
	}
	if err := overrideServiceDirs(corgi, dirPairs); err != nil {
		return err
	}
	byName := indexServices(corgi)
	if err := applyCheckoutPairs(byName, checkoutPairs); err != nil {
		return err
	}
	if err := applyBranchPairs(byName, branchPairs); err != nil {
		return err
	}
	return ApplyFeatureBranch(corgi, feature, pinnedServices(dirPairs, branchPairs, checkoutPairs), nil)
}

// CleanCorgiWorktrees removes every corgi-created worktree (git worktree remove,
// falling back to rm) and prunes the admin entries in each source repo.
func CleanCorgiWorktrees() error {
	base := filepath.Join(CorgiComposePathDir, "corgi_services", ".worktrees")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		dest := filepath.Join(base, e.Name())
		common, cerr := gitOut(dest, gitRevParse, "--path-format=absolute", "--git-common-dir")
		if cerr == nil && common != "" {
			repo := filepath.Dir(common)
			if gitRun(repo, "worktree", "remove", "--force", dest) == nil {
				_ = gitRun(repo, "worktree", "prune")
				continue
			}
		}
		_ = os.RemoveAll(dest)
	}
	return os.RemoveAll(base)
}
