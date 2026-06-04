package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

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

func isTreeDirty(dir string) (bool, error) {
	out, err := gitOut(dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func insideWorktree(dir string) bool {
	out, err := gitOut(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func isGitRepo(dir string) bool {
	_, err := gitOut(dir, "rev-parse", "--git-dir")
	return err == nil
}

func branchSlug(branch string) string {
	return strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(branch)
}

// worktreeDest is deterministic per (service, branch) so re-runs reuse one dir.
func worktreeDest(service, branch string) string {
	return filepath.Join(CorgiComposePathDir, "corgi_services", ".worktrees", service+"-"+branchSlug(branch))
}

func cutServicePair(pair string) (name, val string, err error) {
	name, val, ok := strings.Cut(pair, "=")
	if !ok || name == "" || val == "" {
		return "", "", fmt.Errorf("expects name=value, got %q", pair)
	}
	return name, val, nil
}

// EnsureServiceWorktree prunes stale entries, reuses a healthy worktree at dest,
// and only creates one when missing or broken — keeping deps and uncommitted work.
func EnsureServiceWorktree(repo, branch, dest string) error {
	if !isGitRepo(repo) {
		return fmt.Errorf("%s is not a git repository (run corgi init first)", repo)
	}
	_ = gitRun(repo, "worktree", "prune")
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		if insideWorktree(dest) {
			cur, _ := gitOut(dest, "rev-parse", "--abbrev-ref", "HEAD")
			if cur != branch {
				if err := gitRun(dest, "checkout", branch); err != nil {
					return fmt.Errorf("reuse worktree %s on %s: %v", dest, branch, err)
				}
			}
			return nil
		}
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := gitRun(repo, "worktree", "add", dest, branch); err != nil {
		return fmt.Errorf("git worktree add %s %s: %v", dest, branch, err)
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

// MaterializeServiceWorktrees applies --service-branch (reused worktree) and
// --service-checkout (in-place checkout) by repointing AbsolutePath, the single
// source of every service cwd. Side-effecting (git), so callers run it after any
// dry-run guard.
func MaterializeServiceWorktrees(cmd *cobra.Command, corgi *CorgiCompose) error {
	branchPairs := cmdStringArray(cmd, "service-branch")
	checkoutPairs := cmdStringArray(cmd, "service-checkout")
	if len(branchPairs) == 0 && len(checkoutPairs) == 0 {
		return nil
	}
	if err := assertNoServiceWorkdirConflict(cmd); err != nil {
		return err
	}

	byName := map[string]*Service{}
	for i := range corgi.Services {
		byName[corgi.Services[i].ServiceName] = &corgi.Services[i]
	}

	for _, pair := range checkoutPairs {
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

	for _, pair := range branchPairs {
		name, branch, err := cutServicePair(pair)
		if err != nil {
			return fmt.Errorf("--service-branch %v", err)
		}
		svc, found := byName[name]
		if !found {
			return fmt.Errorf("--service-branch: no service named %q in corgi-compose.yml", name)
		}
		dest := worktreeDest(name, branch)
		if err := EnsureServiceWorktree(svc.AbsolutePath, branch, dest); err != nil {
			return fmt.Errorf("--service-branch %s: %v", name, err)
		}
		Info("service-branch:", name, "→", branch, "@", dest)
		svc.AbsolutePath = dest
	}
	return nil
}

// A service may appear in only one of --service-dir/--service-branch/--service-checkout.
func assertNoServiceWorkdirConflict(cmd *cobra.Command) error {
	seen := map[string]string{}
	for _, flag := range []string{"service-dir", "service-branch", "service-checkout"} {
		for _, pair := range cmdStringArray(cmd, flag) {
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
		common, cerr := gitOut(dest, "rev-parse", "--path-format=absolute", "--git-common-dir")
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
