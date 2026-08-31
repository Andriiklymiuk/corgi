package utils

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

	base := AgentWorktreeBase(composeDir)
	if err := prepareWorktreeBase(composeDir); err != nil {
		return nil, err
	}

	order, byRoot, skipped := groupServicesByRepo(corgi, branch, services)
	results := prepareWorktrees(order, base, branch)

	set := &WorktreeSet{Branch: branch}
	set.Worktrees = append(set.Worktrees, skipped...)
	for i, root := range order {
		for _, svc := range byRoot[root] {
			set.Worktrees = append(set.Worktrees, results[i].entry(svc, root, branch))
		}
	}
	sort.Slice(set.Worktrees, func(i, j int) bool {
		return set.Worktrees[i].Service < set.Worktrees[j].Service
	})
	return set, nil
}

// prepareWorktreeBase creates the worktree directory and keeps it out of git.
// corgi_services/ is not wholly ignored, so each new thing under it must add
// its own entry.
func prepareWorktreeBase(composeDir string) error {
	corgiServices := filepath.Join(composeDir, "corgi_services")
	if err := os.MkdirAll(corgiServices, 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(corgiServices, ".worktrees/")
	return nil
}

// groupServicesByRepo collects the distinct repositories to prepare. Two
// services can share one, and git allows a branch in exactly one worktree, so
// each repository is prepared once and every service on it named afterwards.
func groupServicesByRepo(corgi *CorgiCompose, branch string, services []string) (order []string, byRoot map[string][]string, skipped []RepoWorktree) {
	wanted := map[string]bool{}
	for _, s := range services {
		wanted[strings.TrimSpace(s)] = true
	}
	byRoot = map[string][]string{}

	for i := range corgi.Services {
		svc := &corgi.Services[i]
		if len(wanted) > 0 && !wanted[svc.ServiceName] {
			continue
		}
		root, ok := repoRoot(svc.AbsolutePath)
		if !ok {
			skipped = append(skipped, RepoWorktree{
				Service: svc.ServiceName, Branch: branch, Skipped: "not a git repository",
			})
			continue
		}
		if _, seen := byRoot[root]; !seen {
			order = append(order, root)
		}
		byRoot[root] = append(byRoot[root], svc.ServiceName)
	}
	return order, byRoot, skipped
}

// worktreeResult is one repository's preparation outcome.
type worktreeResult struct {
	dir     string
	created bool
	err     error
}

// entry renders the result for one service.
func (r worktreeResult) entry(service, root, branch string) RepoWorktree {
	e := RepoWorktree{Service: service, Repo: root, Branch: branch}
	if r.err != nil {
		e.Skipped = r.err.Error()
		return e
	}
	e.Dir, e.Created = r.dir, r.created
	return e
}

// prepareWorktrees materializes each repository concurrently.
//
// Each one may consult origin, which is bounded but not instant, and this runs
// inside an MCP handler holding a process-wide lock — so the cost must be the
// slowest repository, never the sum of them.
func prepareWorktrees(order []string, base, branch string) []worktreeResult {
	results := make([]worktreeResult, len(order))
	var wg sync.WaitGroup
	for i, root := range order {
		wg.Add(1)
		go func(i int, root string) {
			defer wg.Done()
			dest := filepath.Join(base, worktreeDirName(root, branch))
			dir, created, err := ensureWorkBranchWorktree(root, branch, dest)
			results[i] = worktreeResult{dir: dir, created: created, err: err}
		}(i, root)
	}
	wg.Wait()
	return results
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

// ExistingBranchWorktrees reports the worktrees a branch already has, without
// creating anything and without touching the network.
//
// corgi_diff uses this: that tool is advertised as read-only and is ungated, so
// it must not become a way around the gate on materialize.
func ExistingBranchWorktrees(corgi *CorgiCompose, composeDir, branch string) (*WorktreeSet, error) {
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}
	base := AgentWorktreeBase(composeDir)
	set := &WorktreeSet{Branch: branch}

	for i := range corgi.Services {
		svc := &corgi.Services[i]
		root, ok := repoRoot(svc.AbsolutePath)
		if !ok {
			continue
		}
		dir := existingWorktreeDir(root, base, branch)
		if dir == "" {
			continue
		}
		set.Worktrees = append(set.Worktrees, RepoWorktree{
			Service: svc.ServiceName, Repo: root, Branch: branch, Dir: dir,
		})
	}
	sort.Slice(set.Worktrees, func(i, j int) bool {
		return set.Worktrees[i].Service < set.Worktrees[j].Service
	})
	return set, nil
}

// ReleaseBranchWorktrees removes the worktrees a branch materialized, leaving
// the branches alone — the work is usually the point. A worktree with
// uncommitted changes is kept and reported rather than removed, since
// `--force` would discard work nobody asked it to. Pass force to override.
func ReleaseBranchWorktrees(composeDir, branch string) ([]string, error) {
	removed, _, err := releaseBranchWorktrees(composeDir, branch, false)
	return removed, err
}

// ReleaseBranchWorktreesReport is ReleaseBranchWorktrees, also reporting which
// worktrees were kept because they held uncommitted changes.
func ReleaseBranchWorktreesReport(composeDir, branch string) (removed, keptDirty []string, err error) {
	return releaseBranchWorktrees(composeDir, branch, false)
}

// ReleaseBranchWorktreesForce removes them even when dirty, and reports which
// held uncommitted work.
func ReleaseBranchWorktreesForce(composeDir, branch string) (removed, wereDirty []string, err error) {
	return releaseBranchWorktrees(composeDir, branch, true)
}

func releaseBranchWorktrees(composeDir, branch string, force bool) (removed, skippedDirty []string, err error) {
	if verr := validateBranchName(branch); verr != nil {
		return nil, nil, verr
	}
	base := AgentWorktreeBase(composeDir)
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, nil, nil
		}
		return nil, nil, rerr
	}

	suffix := "@" + branchDirSegment(branch)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		dest := filepath.Join(base, e.Name())
		// The directory name is a flattened branch, so feature/login and
		// feature-login collide there. Confirm against the real HEAD before
		// force-removing someone else's worktree.
		if head, herr := gitOut(dest, gitRevParse, gitAbbrevRef, "HEAD"); herr == nil && head != branch {
			continue
		}
		if !force && HasUncommittedWork(dest) {
			skippedDirty = append(skippedDirty, dest)
			continue
		}
		if removeWorktree(dest) {
			removed = append(removed, dest)
		}
	}
	sort.Strings(removed)
	sort.Strings(skippedDirty)
	return removed, skippedDirty, nil
}

// existingWorktreeDir resolves where this repo's copy of the branch lives.
// The main checkout counts when already on the branch — that is what
// materialize returns, and requiring the worktree base would report "nothing
// here" for a correctly checked-out repo.
func existingWorktreeDir(root, base, branch string) string {
	if head, err := gitOut(root, gitRevParse, gitAbbrevRef, "HEAD"); err == nil && head == branch {
		return root
	}
	dest := filepath.Join(base, worktreeDirName(root, branch))
	info, statErr := os.Stat(dest)
	if statErr != nil || !info.IsDir() {
		return ""
	}
	if h, herr := gitOut(dest, gitRevParse, gitAbbrevRef, "HEAD"); herr == nil && h == branch {
		return dest
	}
	return ""
}

// removeWorktree unregisters it from the repo, falling back to deleting the
// directory when git will not.
func removeWorktree(dest string) bool {
	common, cerr := gitOut(dest, gitRevParse, "--path-format=absolute", "--git-common-dir")
	if cerr == nil && common != "" {
		repo := filepath.Dir(common)
		if gitRun(repo, "worktree", "remove", "--force", dest) == nil {
			_ = gitRun(repo, "worktree", "prune")
			return true
		}
	}
	return os.RemoveAll(dest) == nil
}

// HasUncommittedWork reports whether a checkout holds anything not committed,
// untracked files included — unlike isTreeDirty, which ignores them. An agent's
// work is usually brand-new files, the ones it would hurt most to discard.
// .gitignore still applies, so build output does not count.
func HasUncommittedWork(dir string) bool {
	out, err := gitOut(dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// worktreeDirName keeps repo and branch in the directory name so a release can
// find exactly the worktrees a branch created. The basename alone is not unique
// — a stack can hold ~/work/api and ~/oss/api — so a hash of the full path is
// appended, or the second service silently gets the first's worktree.
func worktreeDirName(repo, branch string) string {
	return fmt.Sprintf("%s@%s", WorktreeDirPrefix(repo), branchDirSegment(branch))
}

// WorktreeDirPrefix is the part of a worktree directory name before the "@",
// derived only from the repository path. Exported because parsing the name by
// hand does not work: the prefix is "<basename>-<hash>", so splitting on "@"
// yields "api-3f2a1b" rather than "api".
func WorktreeDirPrefix(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return fmt.Sprintf("%s-%x", filepath.Base(repo), sum[:3])
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
