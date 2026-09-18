package sessions

import (
	"os"
	"path/filepath"
	"strings"
)

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

func RepoRoot(dir string) string {
	_, root := findGitDir(dir)
	return root
}

func CommonRoot(dir string) string {
	gitDir, root := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(gitDir))
	if i := strings.LastIndex(clean, "/.git/worktrees/"); i >= 0 {
		return filepath.FromSlash(clean[:i])
	}
	return root
}

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
