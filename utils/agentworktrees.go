package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A corgi stack spans several repositories. Claude Code's Remote Control makes
// one worktree of one repository, so materializing the same branch across every
// repo in a stack is corgi's job.

// RepoWorktree is one repository's checkout for a work branch.
type RepoWorktree struct {
	Service string `json:"service"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Dir     string `json:"dir"`
	Created bool   `json:"created"`
	Skipped string `json:"skipped,omitempty"`
}

// WorktreeSet is the result of materializing a branch across a stack.
type WorktreeSet struct {
	Branch    string         `json:"branch"`
	Worktrees []RepoWorktree `json:"worktrees"`
}

// AgentWorktreeBase is where corgi puts agent worktrees. Kept under
// corgi_services so the existing prune and gitignore handling applies.
func AgentWorktreeBase(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".worktrees")
}

// MaterializeBranchAcrossRepos gives every named service's repository a
// worktree on branch, creating the branch off that repo's current HEAD when it
// does not exist yet.
//
// services empty means every service in the stack. Two services sharing one
// repository share one worktree, because git allows a branch in exactly one.
func MaterializeBranchAcrossRepos(corgi *CorgiCompose, composeDir, branch string, services []string) (*WorktreeSet, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}

	wanted := map[string]bool{}
	for _, s := range services {
		wanted[strings.TrimSpace(s)] = true
	}

	base := AgentWorktreeBase(composeDir)
	corgiServices := filepath.Join(composeDir, "corgi_services")
	if err := os.MkdirAll(corgiServices, 0o755); err != nil {
		return nil, err
	}
	// corgi_services/ is not wholly gitignored, so each new thing under it must
	// add its own entry.
	EnsureCorgiServicesIgnore(corgiServices, ".worktrees/")

	set := &WorktreeSet{Branch: branch}
	byRepo := map[string]string{} // repo root → worktree dir, so shared repos agree

	for i := range corgi.Services {
		svc := &corgi.Services[i]
		if len(wanted) > 0 && !wanted[svc.ServiceName] {
			continue
		}
		entry := RepoWorktree{Service: svc.ServiceName, Branch: branch}

		root, ok := repoRoot(svc.AbsolutePath)
		if !ok {
			entry.Skipped = "not a git repository"
			set.Worktrees = append(set.Worktrees, entry)
			continue
		}
		entry.Repo = root

		if dir, seen := byRepo[root]; seen {
			entry.Dir = dir
			set.Worktrees = append(set.Worktrees, entry)
			continue
		}

		dest := filepath.Join(base, worktreeDirName(root, branch))
		dir, created, err := ensureWorkBranchWorktree(root, branch, dest)
		if err != nil {
			entry.Skipped = err.Error()
			set.Worktrees = append(set.Worktrees, entry)
			continue
		}
		entry.Dir, entry.Created = dir, created
		byRepo[root] = dir
		set.Worktrees = append(set.Worktrees, entry)
	}

	sort.Slice(set.Worktrees, func(i, j int) bool {
		return set.Worktrees[i].Service < set.Worktrees[j].Service
	})
	return set, nil
}

// ensureWorkBranchWorktree returns a worktree for branch, creating the branch
// off the repo's current HEAD when nothing carries it yet. Reports whether the
// branch was created.
func ensureWorkBranchWorktree(repo, branch, dest string) (dir string, created bool, err error) {
	if !isGitRepo(repo) {
		return "", false, fmt.Errorf("%s is not a git repository", repo)
	}
	if local, remote := branchIsKnown(repo, branch); local || remote {
		// Something already carries it; reuse rather than fork a second one.
		dir, err = EnsureFeatureWorktree(repo, branch, dest)
		if err != nil {
			return "", false, err
		}
		if dir == "" {
			return "", false, fmt.Errorf("branch %s not available in %s", branch, repo)
		}
		return dir, false, nil
	}

	_ = gitRun(repo, "worktree", "prune")
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		reused, rerr := reuseOrRemoveWorktreeDir(dest, branch)
		if rerr != nil {
			return "", false, rerr
		}
		if reused != "" {
			return reused, false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", false, err
	}
	if err := gitRun(repo, "worktree", "add", "-b", branch, dest); err != nil {
		return "", false, fmt.Errorf("git worktree add -b %s %s: %v", branch, dest, err)
	}
	return dest, true, nil
}

// ReleaseBranchWorktrees removes the worktrees a branch materialized, leaving
// the branches themselves alone — the work is usually the point.
func ReleaseBranchWorktrees(composeDir, branch string) ([]string, error) {
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}
	base := AgentWorktreeBase(composeDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	suffix := "@" + branchDirSegment(branch)
	var removed []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		dest := filepath.Join(base, e.Name())
		common, cerr := gitOut(dest, gitRevParse, "--path-format=absolute", "--git-common-dir")
		if cerr == nil && common != "" {
			repo := filepath.Dir(common)
			if gitRun(repo, "worktree", "remove", "--force", dest) == nil {
				_ = gitRun(repo, "worktree", "prune")
				removed = append(removed, dest)
				continue
			}
		}
		if rmErr := os.RemoveAll(dest); rmErr == nil {
			removed = append(removed, dest)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// worktreeDirName keeps repo and branch in the directory name so a release can
// find exactly the worktrees a branch created.
func worktreeDirName(repo, branch string) string {
	return filepath.Base(repo) + "@" + branchDirSegment(branch)
}

// branchDirSegment flattens a branch name into one path segment.
func branchDirSegment(branch string) string {
	return strings.NewReplacer("/", "-", string(filepath.Separator), "-").Replace(branch)
}

// validateBranchName rejects names git would refuse or that would escape the
// worktree base directory.
func validateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	switch {
	case branch == "":
		return fmt.Errorf("branch is required")
	case strings.HasPrefix(branch, "-"):
		return fmt.Errorf("branch %q cannot start with a dash", branch)
	case strings.Contains(branch, ".."):
		return fmt.Errorf("branch %q cannot contain ..", branch)
	case strings.ContainsAny(branch, " ~^:?*[\\\t\n"):
		return fmt.Errorf("branch %q contains characters git does not allow", branch)
	case strings.HasSuffix(branch, "/") || strings.HasPrefix(branch, "/"):
		return fmt.Errorf("branch %q cannot start or end with a slash", branch)
	}
	return nil
}
