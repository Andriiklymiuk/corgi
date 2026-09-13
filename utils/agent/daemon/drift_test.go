package daemon

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/scope"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// Drift is the numbers, not a feeling: a nearly full context, the same tool
// failing again and again, a diff twice the budget, files outside the scope.
func TestDriftIsReadFromTheNumbers(t *testing.T) {
	origDiff, origRoot := driftDiff, workspaceRootOf
	defer func() { driftDiff, workspaceRootOf = origDiff, origRoot }()
	root := t.TempDir()
	workspaceRootOf = func(string) string { return root }
	if err := scope.Write(root, scope.Scope{Ref: "ABC-4", Paths: []string{"api/**"}, Lines: 200}); err != nil {
		t.Fatal(err)
	}
	driftDiff = func(string) (int, []string, bool) { return 120, []string{"api/a.go"}, true }

	calm := sessions.Session{Cwd: root, Branch: "feature/ABC-4/x", Context: &usage.Context{Percent: 40}}
	if r := driftReasons(calm); len(r) != 0 {
		t.Fatalf("calm: %v", r)
	}
	loud := calm
	loud.Context = &usage.Context{Percent: 91}
	loud.FailStreak = 4
	driftDiff = func(string) (int, []string, bool) { return 500, []string{"api/a.go", "web/b.tsx", "web/c.tsx"}, true }
	r := driftReasons(loud)
	if len(r) != 4 {
		t.Fatalf("four reasons: %v", r)
	}
	for i, want := range []string{"context 91%", "failed 4 times", "500 lines, twice the 200-line budget", "2 file(s) outside the scope for ABC-4: web/b.tsx, web/c.tsx"} {
		if !strings.Contains(r[i], want) {
			t.Errorf("reason %d lacks %q: %s", i, want, r[i])
		}
	}
	// No scope: only the floor applies.
	noScope := sessions.Session{Cwd: root, Branch: "main"}
	driftDiff = func(string) (int, []string, bool) { return 1200, []string{"x"}, true }
	if r := driftReasons(noScope); len(r) != 0 {
		t.Fatalf("1200 lines with no scope is a feature, not drift: %v", r)
	}
	driftDiff = func(string) (int, []string, bool) { return 5000, []string{"x"}, true }
	r = driftReasons(noScope)
	if len(r) != 1 || !strings.Contains(r[0], "far past the usual size") {
		t.Fatalf("over the floor: %v", r)
	}
	// What the diff says shows on the board and never rings; a full context
	// or a tool failing on repeat does.
	if l, q := driftReasonsSplit(noScope); len(l) != 0 || len(q) != 1 {
		t.Errorf("a big diff is quiet: loud=%v quiet=%v", l, q)
	}
	if l, q := driftReasonsSplit(loud); len(l) != 2 || len(q) != 2 || !strings.Contains(l[0], "context 91%") {
		t.Errorf("context and the fail streak ring, the diff does not: loud=%v quiet=%v", l, q)
	}
}

// A lock file or a bundle is nobody's work: it counts for scope, never for
// the size of the diff.
func TestGeneratedFilesDoNotCountAsDiffSize(t *testing.T) {
	for _, p := range []string{"package-lock.json", "app/yarn.lock", "web/dist/app.js", "api/__snapshots__/a.snap", "pkg/x.pb.go", "site/main.min.js"} {
		if !GeneratedDiffPath(p) {
			t.Errorf("%s should not count", p)
		}
	}
	for _, p := range []string{"src/app/pair.tsx", "cmd/root.go", "Makefile", "docs/build.md"} {
		if GeneratedDiffPath(p) {
			t.Errorf("%s is real work", p)
		}
	}
}
