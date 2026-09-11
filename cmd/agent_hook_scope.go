package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/scope"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// Two hooks keep a session inside the scope the spec agreed. PreToolUse on
// a write refuses a path outside it, with the way to widen; Stop reports a
// diff over budget once, so the size is a decision and not a surprise at
// review. Both are silent when the branch has no scope, so a workspace that
// never wrote one is untouched.

type scopeHookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
	StopHookActive bool `json:"stop_hook_active"`
}

// scopeFor finds the workspace root and the scope for the branch cwd is on.
func scopeFor(cwd string) (root string, s scope.Scope, ok bool) {
	root = scopeWorkspaceRoot(cwd)
	if root == "" {
		return "", scope.Scope{}, false
	}
	s, ok = scope.ForBranch(root, sessions.Branch(cwd))
	return root, s, ok
}

// workspaceRootFor is the registered workspace containing cwd, else the git
// root, else "".
func scopeWorkspaceRoot(cwd string) string {
	if dir := agentDirOrEmpty(); dir != "" {
		if registry, err := workspace.Load(agentRegistryPath(dir)); err == nil {
			if _, root := workspaceLabel(registry, cwd); root != "" {
				return root
			}
		}
	}
	return sessions.RepoRoot(cwd)
}

func runScopeHook(stdin io.Reader, stdout io.Writer) {
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return
	}
	var in scopeHookInput
	if json.Unmarshal(data, &in) != nil || in.Cwd == "" {
		return
	}
	file := firstNonEmpty(in.ToolInput.FilePath, in.ToolInput.NotebookPath)
	if file == "" {
		return
	}
	root, s, ok := scopeFor(in.Cwd)
	if !ok || len(s.Paths) == 0 {
		return
	}
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(in.Cwd, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = abs
	}
	// Named the way the scope was written: "<repo>/<inside>" for a file in
	// a service repository or a worktree, plain for a monorepo.
	if repoRoot := sessions.RepoRoot(filepath.Dir(abs)); repoRoot != "" {
		if named := scope.InRepo(root, repoRoot, abs); named != "" {
			rel = named
		}
	}
	if s.Allows(rel) {
		return
	}
	reason := fmt.Sprintf("%s is outside the scope agreed for %s (%s). If the change really needs it, widen the scope on the record — `corgi agent scope add %s --path %q` — and say why in the PR; otherwise keep the change inside it.",
		rel, s.Ref, strings.Join(s.Paths, ", "), s.Ref, rel)
	_ = json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
}

// diffStat is a seam for tests: added+removed lines and new test files
// against the branch's base.
var diffStat = gitDiffStat

func gitDiffStat(dir string) (lines int, newTests int, ok bool) {
	base := ""
	for _, b := range []string{"origin/main", "origin/master", "main", "master"} {
		out, err := exec.Command("git", "-C", dir, "merge-base", "HEAD", b).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			base = strings.TrimSpace(string(out))
			break
		}
	}
	if base == "" {
		return 0, 0, false
	}
	out, err := exec.Command("git", "-C", dir, "diff", "--numstat", base).Output()
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		lines += a + d
	}
	added, err := exec.Command("git", "-C", dir, "diff", "--name-only", "--diff-filter=A", base).Output()
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(added)), "\n") {
			if f != "" && scope.IsTestFile(f) {
				newTests++
			}
		}
	}
	return lines, newTests, true
}

func runBudgetHook(stdin io.Reader, stdout io.Writer) {
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return
	}
	var in scopeHookInput
	if json.Unmarshal(data, &in) != nil || in.Cwd == "" || in.StopHookActive {
		return
	}
	root, s, ok := scopeFor(in.Cwd)
	if !ok || (s.Lines == 0 && s.Tests == 0) {
		return
	}
	lines, tests, ok := diffStat(in.Cwd)
	if !ok {
		return
	}
	var over []string
	if s.Lines > 0 && lines > s.Lines {
		over = append(over, fmt.Sprintf("the diff is %d lines against a budget of %d", lines, s.Lines))
	}
	if s.Tests > 0 && tests > s.Tests {
		over = append(over, fmt.Sprintf("%d new test files against a budget of %d", tests, s.Tests))
	}
	if len(over) == 0 {
		return
	}
	// Once per session: the second Stop lets the turn end, so a size the
	// run has already defended is not a loop.
	marker := filepath.Join(scope.Dir(root), "."+in.SessionID+"-"+s.Ref+".reported")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	sweepOldMarkers(scope.Dir(root))
	_ = os.WriteFile(marker, []byte("reported\n"), 0o600)
	reason := fmt.Sprintf("Before you stop: %s for %s. Trim the change to what the spec asked for, or — if this size is right — say why in one line in the PR body and raise the budget on the record: `corgi agent scope set %s --lines %d --tests %d`.",
		strings.Join(over, "; "), s.Ref, s.Ref, lines, tests)
	_ = json.NewEncoder(stdout).Encode(map[string]any{"decision": "block", "reason": reason})
}

// sweepOldMarkers drops reported-markers older than a week, so the scope
// directory does not fill with one file per session forever.
func sweepOldMarkers(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".reported") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 7*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
