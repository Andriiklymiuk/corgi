package sessions

import (
	"testing"
	"time"
)

// The last test run is the first thing a person reads when a session says
// it is done: green and when, or red and what.
func TestTheLastTestRunIsOnTheSession(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("UserPromptSubmit", "s1", 0))
	run := ev("PostToolUse", "s1", time.Second)
	run.Tool, run.Subject = "Bash", "go test"
	r.Apply(run)
	s := find(t, r, "s1")
	if s.Tests == nil || !s.Tests.OK || s.Tests.Cmd != "go test" {
		t.Fatalf("a green go test must be on the session: %+v", s.Tests)
	}
	failed := ev("PostToolUseFailure", "s1", 2*time.Second)
	failed.Tool, failed.Subject = "Bash", "bun test"
	r.Apply(failed)
	if s = find(t, r, "s1"); s.Tests == nil || s.Tests.OK || s.Tests.Cmd != "bun test" {
		t.Fatalf("a red bun test must replace it: %+v", s.Tests)
	}
	// A Bash that is not a test run leaves the last test run alone.
	other := ev("PostToolUse", "s1", 3*time.Second)
	other.Tool, other.Subject = "Bash", "git status"
	r.Apply(other)
	if s = find(t, r, "s1"); s.Tests == nil || s.Tests.Cmd != "bun test" {
		t.Fatalf("git status is not a test run: %+v", s.Tests)
	}
}

func TestWhatCountsAsATestCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"go test": true, "npm test": true, "bun test": true, "bunx jest": true, "npx vitest": true, "pytest": true,
		"make check": true, "cargo test": true, "bun run test": true, "npm run test:cov": true, "maestro": true,
		"git status": false, "npm install": false, "go build": false, "bun run dev": false, "": false, "make": false,
	} {
		if got := IsTestCommand(cmd); got != want {
			t.Errorf("IsTestCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// SetChanges reports a change only when something a person would see moved,
// so a board that has not moved is not rewritten every minute.
func TestChangesAreWrittenOnlyWhenTheyMove(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("UserPromptSubmit", "s1", 0))
	c := &Changes{Files: 3, Lines: 120, Touched: []string{"a.go", "b.go"}}
	if changed, _ := r.SetChanges("s1", c, nil); !changed {
		t.Fatal("the first measurement is news")
	}
	if changed, _ := r.SetChanges("s1", &Changes{Files: 3, Lines: 120, Touched: []string{"a.go", "b.go"}, At: time.Now()}, nil); changed {
		t.Fatal("the same numbers a minute later are not")
	}
	changed, crossed := r.SetChanges("s1", c, []Overlap{{ID: "s2", Session: "api·2", Files: []string{"a.go"}}})
	if !changed || !crossed {
		t.Fatal("somebody else on a.go is news, and the moment it starts is worth a ring")
	}
	if changed, crossed := r.SetChanges("s1", c, []Overlap{{ID: "s2", Session: "api·2", Files: []string{"a.go"}}}); changed || crossed {
		t.Fatal("the same overlap again is not")
	}
}

func find(t *testing.T, r *Registry, id string) Session {
	t.Helper()
	s, err := r.Lookup(id)
	if err != nil {
		t.Fatalf("lookup %s: %v", id, err)
	}
	return s
}
