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

type RepoWorktree struct {
	Service string `json:"service"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Dir     string `json:"dir"`
	Created bool   `json:"created"`
	Skipped string `json:"skipped,omitempty"`
}

type WorktreeSet struct {
	Branch    string         `json:"branch"`
	Worktrees []RepoWorktree `json:"worktrees"`
}

func AgentWorktreeBase(composeDir string) string {
	return filepath.Join(CorgiServicesIn(composeDir), ".worktrees")
}

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

func prepareWorktreeBase(composeDir string) error {
	corgiServices := CorgiServicesIn(composeDir)
	if err := os.MkdirAll(corgiServices, 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(corgiServices, ".worktrees/")
	return nil
}

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

type worktreeResult struct {
	dir     string
	created bool
	err     error
}

func (r worktreeResult) entry(service, root, branch string) RepoWorktree {
	e := RepoWorktree{Service: service, Repo: root, Branch: branch}
	if r.err != nil {
		e.Skipped = r.err.Error()
		return e
	}
	e.Dir, e.Created = r.dir, r.created
	return e
}

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

func ensureWorkBranchWorktree(repo, branch, dest string) (dir string, created bool, err error) {
	if !isGitRepo(repo) {
		return "", false, fmt.Errorf("%s is not a git repository", repo)
	}
	if local, remote := branchIsKnown(repo, branch); local || remote {
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

func MaterializeBranchInRepo(repo, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("branch is required")
	}
	if err := validateBranchName(branch); err != nil {
		return "", err
	}
	root := repo
	if r, ok := RepoRootOf(repo); ok && r != "" {
		root = r
	}
	if err := prepareWorktreeBase(root); err != nil {
		return "", err
	}
	dest := filepath.Join(AgentWorktreeBase(root), worktreeDirName(root, branch))
	dir, _, err := ensureWorkBranchWorktree(root, branch, dest)
	return dir, err
}

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

func ReleaseBranchWorktrees(composeDir, branch string) ([]string, error) {
	removed, _, err := releaseBranchWorktrees(composeDir, branch, false)
	return removed, err
}

func ReleaseBranchWorktreesReport(composeDir, branch string) (removed, keptDirty []string, err error) {
	return releaseBranchWorktrees(composeDir, branch, false)
}

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

func HasUncommittedWork(dir string) bool {
	out, err := gitOut(dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

func worktreeDirName(repo, branch string) string {
	return fmt.Sprintf("%s@%s", WorktreeDirPrefix(repo), branchDirSegment(branch))
}

func WorktreeDirPrefix(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return fmt.Sprintf("%s-%x", filepath.Base(repo), sum[:3])
}

func branchDirSegment(branch string) string {
	return strings.NewReplacer("/", "-", string(filepath.Separator), "-").Replace(branch)
}

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
