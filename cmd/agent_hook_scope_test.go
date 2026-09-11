package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/scope"
)

// A write outside the scope is refused with the way to widen; a write
// inside, or on a branch with no scope, passes without a word.
func TestTheScopeHookRefusesAWriteOutsideThePaths(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	root := t.TempDir()
	gitRepoOnBranch(t, root, "feature/ABC-9/limits")
	if err := scope.Write(root, scope.Scope{Ref: "ABC-9", Paths: []string{"api/**"}}); err != nil {
		t.Fatal(err)
	}
	run := func(file string) string {
		in, _ := json.Marshal(map[string]any{"cwd": root, "tool_name": "Edit", "tool_input": map[string]any{"file_path": file}})
		var out bytes.Buffer
		runScopeHook(bytes.NewReader(in), &out)
		return out.String()
	}
	if got := run(filepath.Join(root, "api", "limits.go")); got != "" {
		t.Fatalf("inside: %s", got)
	}
	got := run(filepath.Join(root, "web", "Banner.tsx"))
	if !strings.Contains(got, `"permissionDecision":"deny"`) || !strings.Contains(got, "corgi agent scope add ABC-9") {
		t.Fatalf("outside: %s", got)
	}
	if _, err := scope.Widen(root, "ABC-9", "web/**", "session"); err != nil {
		t.Fatal(err)
	}
	if got := run(filepath.Join(root, "web", "Banner.tsx")); got != "" {
		t.Fatalf("widened: %s", got)
	}
	other := t.TempDir()
	gitRepoOnBranch(t, other, "main")
	in, _ := json.Marshal(map[string]any{"cwd": other, "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(other, "x.go")}})
	var out bytes.Buffer
	runScopeHook(bytes.NewReader(in), &out)
	if out.String() != "" {
		t.Fatal("no scope, no opinion")
	}
}

// A diff over budget stops the turn once with the numbers and the way to
// raise the budget; the second stop in the same session lets it end.
func TestTheBudgetHookReportsOnce(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	root := t.TempDir()
	gitRepoOnBranch(t, root, "feature/ABC-9/limits")
	if err := scope.Write(root, scope.Scope{Ref: "ABC-9", Lines: 100, Tests: 1}); err != nil {
		t.Fatal(err)
	}
	orig := diffStat
	defer func() { diffStat = orig }()
	diffStat = func(string) (int, int, bool) { return 340, 3, true }
	run := func(active bool) string {
		in, _ := json.Marshal(map[string]any{"cwd": root, "session_id": "s1", "stop_hook_active": active})
		var out bytes.Buffer
		runBudgetHook(bytes.NewReader(in), &out)
		return out.String()
	}
	got := run(false)
	if !strings.Contains(got, `"decision":"block"`) || !strings.Contains(got, "340 lines against a budget of 100") || !strings.Contains(got, "3 new test files against a budget of 1") {
		t.Fatalf("first stop: %s", got)
	}
	if got := run(false); got != "" {
		t.Fatalf("second stop in the session passes: %s", got)
	}
	if got := run(true); got != "" {
		t.Fatal("a stop the hook itself caused is never blocked again")
	}
	diffStat = func(string) (int, int, bool) { return 40, 0, true }
	in, _ := json.Marshal(map[string]any{"cwd": root, "session_id": "s2"})
	var out bytes.Buffer
	runBudgetHook(bytes.NewReader(in), &out)
	if out.String() != "" {
		t.Fatal("under budget, silent")
	}
}
