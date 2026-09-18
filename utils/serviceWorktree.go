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

const (
	gitRevParse  = "rev-parse"
	gitAbbrevRef = "--abbrev-ref"
)

const (
	flagServiceDir      = "service-dir"
	flagServiceBranch   = "service-branch"
	flagServiceCheckout = "service-checkout"
)

const featurePrefix = "feature:"

func gitRun(dir string, args ...string) error {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func noPromptEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
}

func gitRunNoPrompt(dir string, args ...string) error {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = noPromptEnv()
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
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

func worktreeDest(service, branch string) string {
	return filepath.Join(worktreesBase(), service+"-"+branchSlug(branch))
}

func worktreesBase() string {
	return filepath.Join(CorgiServicesDir(), ".worktrees")
}

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
		dir, err := reuseOrRemoveWorktreeDir(dest, branch)
		if err != nil {
			return "", err
		}
		if dir != "" {
			return dir, nil
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

func reuseOrRemoveWorktreeDir(dest, branch string) (string, error) {
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
	return "", nil
}

func preferSpelling(path, dest string) string {
	a, okA := realPath(path)
	b, okB := realPath(dest)
	if okA && okB && a == b {
		return dest
	}
	return path
}

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
		if err := applyCheckoutPair(byName, pair); err != nil {
			return err
		}
	}
	return nil
}

func applyCheckoutPair(byName map[string]*Service, pair string) error {
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

func gitProbeRemote(repo, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteProbeTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", repo, "ls-remote", "--heads", "origin", branch)
	c.Env = noPromptEnv()
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

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

func RepoRootOf(dir string) (string, bool) { return repoRoot(dir) }

func repoRoot(dir string) (string, bool) {
	out, err := gitOut(dir, gitRevParse, "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return realPath(out)
}

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
	if err := checkoutKnownBranch(repo, branch, local); err != nil {
		return false, err
	}
	return true, nil
}

func checkoutKnownBranch(repo, branch string, local bool) error {
	if !local {
		if err := fetchBranchFromOrigin(repo, branch); err != nil {
			return err
		}
		if err := gitRun(repo, "checkout", "-B", branch, "refs/remotes/origin/"+branch); err != nil {
			return fmt.Errorf("checkout %s: %v", branch, err)
		}
		return nil
	}
	if err := gitRun(repo, "checkout", branch); err != nil {
		return fmt.Errorf("checkout %s: %v", branch, err)
	}
	return nil
}

func fetchBranchFromOrigin(repo, branch string) error {
	spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	args := []string{"fetch", "--no-tags"}
	if isShallowRepo(repo) {
		args = append(args, "--depth", "1")
	}
	args = append(args, "origin", spec)
	if err := gitRunNoPrompt(repo, args...); err != nil {
		return fmt.Errorf("fetch origin %s: %v", branch, err)
	}
	return nil
}

func EnsureFeatureWorktree(repo, branch, dest string) (string, error) {
	if !isGitRepo(repo) {
		return "", nil
	}
	local, remote := branchIsKnown(repo, branch)
	if !local && !remote {
		return "", nil
	}
	if !local {
		if err := fetchBranchFromOrigin(repo, branch); err != nil {
			return "", err
		}
	}
	return EnsureServiceWorktree(repo, branch, dest)
}

func ApplyFeatureBranch(corgi *CorgiCompose, branch string, pinned, only map[string]bool) error {
	if branch == "" {
		return nil
	}
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

func ApplyServiceWorkdirs(corgi *CorgiCompose, dirPairs, branchPairs, checkoutPairs []string) error {
	return ApplyServiceWorkdirsWithFeature(corgi, dirPairs, branchPairs, checkoutPairs, "")
}

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

func CleanCorgiWorktrees(force bool) ([]string, error) {
	return CleanWorktreesUnder(filepath.Join(CorgiServicesDir(), ".worktrees"), force)
}

func CleanWorktreesUnder(base string, force bool) ([]string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skipped []string
	for i, e := range entries {
		dest := filepath.Join(base, e.Name())
		if !force && HasUncommittedWork(dest) {
			skipped = append(skipped, dest)
			Infof("[%d/%d] kept %s (uncommitted changes)\n", i+1, len(entries), e.Name())
			continue
		}
		Infof("[%d/%d] removing %s\n", i+1, len(entries), e.Name())
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
	if len(skipped) > 0 {
		return skipped, nil
	}
	return nil, os.RemoveAll(base)
}
