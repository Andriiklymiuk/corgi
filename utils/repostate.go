package utils

import (
	"strconv"
	"strings"
)

func ReadRepoState(dir string) (RepoState, bool) {
	state, ok := ProbeRepoState(dir)
	if !ok {
		return state, false
	}
	state.Path = dir
	if root, rooted := repoRoot(dir); rooted {
		state.Path = root
	}
	state.Head, _ = gitOut(dir, gitRevParse, "--short", "HEAD")
	if upstream, err := gitOut(dir, gitRevParse, gitAbbrevRef, "--symbolic-full-name", "@{u}"); err == nil {
		state.Upstream = upstream
		state.Ahead, state.Behind = countAheadBehind(dir, upstream)
	}
	return state, true
}

func countAheadBehind(dir, upstream string) (ahead, behind int) {
	out, err := gitOut(dir, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return ahead, behind
}

func RepoChangedSince(dir, base string) (changed, known bool) {
	if dir == "" || base == "" || !isGitRepo(dir) {
		return false, false
	}
	ref, ok := resolveBaseRef(dir, base)
	if !ok {
		return false, false
	}
	if out, err := gitOut(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		return true, true
	}
	out, err := gitOut(dir, "rev-list", "--count", ref+"..HEAD")
	if err != nil {
		return false, false
	}
	trimmed := strings.TrimSpace(out)
	return trimmed != "" && trimmed != "0", true
}

func resolveBaseRef(dir, base string) (string, bool) {
	for _, ref := range []string{base, "origin/" + base} {
		if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", ref); err == nil {
			return ref, true
		}
	}
	return "", false
}

func RepoHead(dir string) (string, error) {
	return gitOut(dir, gitRevParse, "HEAD")
}

// CurrentBranch reads just the branch a checkout is on: no network, and none
// of ProbeRepoState's status scan, which walks the whole worktree. Returns ""
// for a directory that is not a git repository and for a detached HEAD, where
// there is no branch name to show.
func CurrentBranch(dir string) string {
	if dir == "" || !isGitRepo(dir) {
		return ""
	}
	branch, err := gitOut(dir, gitRevParse, gitAbbrevRef, "HEAD")
	if err != nil || branch == "HEAD" {
		return ""
	}
	return branch
}
