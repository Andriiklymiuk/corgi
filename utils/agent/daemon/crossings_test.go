package daemon

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// Two sessions on one repository, each in its own worktree, both editing
// registry.go: the board says so before a merge does. Two in the same
// checkout share every file, and the board says that instead. A session in
// another repository is nobody's business here.
func TestCrossingsNameWhoElseIsOnTheSameFiles(t *testing.T) {
	live := []sessions.Session{
		{ID: "a", Label: "api", Display: "api"},
		{ID: "b", Label: "api", Display: "api·2"},
		{ID: "c", Label: "api", Display: "api·3"},
		{ID: "d", Label: "web", Display: "web"},
	}
	measured := map[string]measure{
		"a": {ok: true, repo: "/r/api", tree: "/r/api/.wt/one", files: []string{"registry.go", "a.go", "go.sum"}},
		"b": {ok: true, repo: "/r/api", tree: "/r/api/.wt/two", files: []string{"registry.go", "b.go", "go.sum"}},
		"c": {ok: true, repo: "/r/api", tree: "/r/api/.wt/two", files: []string{"registry.go", "b.go", "go.sum"}},
		"d": {ok: true, repo: "/r/web", tree: "/r/web", files: []string{"registry.go"}},
	}
	got := crossings(live, measured)

	a := got["a"]
	if len(a) != 2 {
		t.Fatalf("api overlaps with two others: %+v", a)
	}
	for _, o := range a {
		if o.SameCheckout || strings.Join(o.Files, ",") != "registry.go" {
			t.Errorf("api shares registry.go with %s, and only that (go.sum is nobody's work): %+v", o.Session, o)
		}
	}
	b := got["b"]
	var same, files int
	for _, o := range b {
		if o.SameCheckout {
			same++
			if o.Session != "api·3" {
				t.Errorf("api·2 shares its checkout with api·3, not %s", o.Session)
			}
		} else {
			files++
		}
	}
	if same != 1 || files != 1 {
		t.Fatalf("api·2: one same-checkout twin and one file overlap, got %+v", b)
	}
	if len(got["d"]) != 0 {
		t.Fatalf("web is in another repository: %+v", got["d"])
	}
}

// The sweep puts the numbers on the board, and a session in another
// working tree of the same repository shows up as an overlap.
func TestTheSweepWritesChangesAndOverlap(t *testing.T) {
	origDiff, origTree, origRoot := driftDiff, workingTreeOf, workspaceRootOf
	defer func() { driftDiff, workingTreeOf, workspaceRootOf = origDiff, origTree, origRoot }()
	d := testDaemon(t)
	d.Sessions = sessions.New(t.TempDir()+"/sessions.json", 8)
	root := t.TempDir()
	workspaceRootOf = func(string) string { return root }
	for _, id := range []string{"one", "two"} {
		start := sessions.Event{Name: "SessionStart", SessionID: id, Cwd: root + "/" + id, Source: "startup"}
		d.Sessions.Apply(start)
		d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: id, Cwd: root + "/" + id})
	}
	driftDiff = func(dir string) (int, []string, bool) {
		return 40, []string{"shared.go", strings.TrimPrefix(dir, root)}, true
	}
	workingTreeOf = func(dir string) string { return dir }

	d.checkDrift(time.Now())

	for _, id := range []string{"one", "two"} {
		s, err := d.Sessions.Lookup(id)
		if err != nil {
			t.Fatal(err)
		}
		if s.Changes == nil || s.Changes.Files != 2 || s.Changes.Lines != 40 {
			t.Fatalf("%s: changes must be on the board: %+v", id, s.Changes)
		}
		if len(s.Overlap) != 1 || strings.Join(s.Overlap[0].Files, ",") != "shared.go" {
			t.Fatalf("%s: the other session is on shared.go too: %+v", id, s.Overlap)
		}
	}
}
