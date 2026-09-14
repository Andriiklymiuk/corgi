package sessions

import (
	"os"
	"path/filepath"
	"strings"
)

// Branch is the checked-out branch of the repository containing dir, read
// from .git without running git. Empty outside a repository; a detached
// head gives the short hash.
func Branch(dir string) string {
	gitDir, _ := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(head))
	if strings.HasPrefix(ref, "ref: ") {
		return strings.TrimPrefix(ref, "ref: refs/heads/")
	}
	if len(ref) >= 12 {
		return ref[:12]
	}
	return ref
}

// RepoRoot is the directory holding the nearest .git above dir (a worktree
// counts), or "" outside a repository.
func RepoRoot(dir string) string {
	_, root := findGitDir(dir)
	return root
}

// CommonRoot is the repository a checkout belongs to, whichever worktree
// it is: the main checkout's root for a worktree, the checkout itself
// otherwise. Two sessions in two worktrees of one repository share it —
// which is what a claim on a file is about.
func CommonRoot(dir string) string {
	gitDir, root := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	// A worktree's git dir is <main>/.git/worktrees/<name>.
	clean := filepath.ToSlash(filepath.Clean(gitDir))
	if i := strings.LastIndex(clean, "/.git/worktrees/"); i >= 0 {
		return filepath.FromSlash(clean[:i])
	}
	return root
}

// findGitDir walks up from dir to the nearest .git, following a worktree's
// "gitdir:" pointer file. Returns the git directory and the checkout root.
func findGitDir(dir string) (string, string) {
	for i := 0; i < 32 && dir != ""; i++ {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return candidate, dir
			}
			data, err := os.ReadFile(candidate)
			if err != nil {
				return "", ""
			}
			target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
			if target == "" {
				return "", ""
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return target, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ""
}
