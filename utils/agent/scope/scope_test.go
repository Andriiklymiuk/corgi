package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeRoundTripsAndMatches(t *testing.T) {
	dir := t.TempDir()
	s := Scope{Ref: "ABC-1", Paths: []string{"api/limits/**", "web/src/limits/*.tsx", "docs/limits.md", "shared/"}, Lines: 400, Tests: 2, Done: []string{"429 with Retry-After"}}
	if err := Write(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, "ABC-1")
	if err != nil || got.Lines != 400 || len(got.Paths) != 4 {
		t.Fatalf("read back: %+v %v", got, err)
	}
	ignore, _ := os.ReadFile(filepath.Join(dir, ".corgi", "corgi_services", ".gitignore"))
	if !strings.Contains(string(ignore), "scope/") {
		t.Fatal("scope is per-developer state")
	}
	for rel, want := range map[string]bool{
		"api/limits/handler.go":     true,
		"api/limits/deep/inner.go":  true,
		"api/other.go":              false,
		"web/src/limits/Banner.tsx": true,
		"web/src/limits/x/Deep.tsx": false,
		"docs/limits.md":            true,
		"docs/other.md":             false,
		"shared/types.ts":           true,
		"web/package.json":          false,
	} {
		if got.Allows(rel) != want {
			t.Errorf("%s: allowed=%v, want %v", rel, got.Allows(rel), want)
		}
	}
	if _, ok := ForBranch(dir, "feature/abc-1/limits"); !ok {
		t.Fatal("found by branch")
	}
	if s, err := Widen(dir, "ABC-1", "web/package.json", "run"); err != nil || !s.Allows("web/package.json") || len(s.Widenings) != 1 {
		t.Fatalf("widen: %+v %v", s, err)
	}
	inWorktree := Scope{Ref: "X", Paths: []string{"api/limits/**", "shared/**"}}
	for rel, want := range map[string]bool{
		".corgi/corgi_services/.worktrees/api-3f2a1b@feature-x/limits/h.go": true,  // a worktree of the api repo, seen from the root
		".corgi/corgi_services/.worktrees/web-3f2a1b@feature-x/limits/h.go": false, // a worktree of another repo
		"api/limits/h.go":        true,  // a monorepo, from the root
		"web/limits/h.go":        false, // a different repo
		"services/shared/x.ts":   true,  // the glob was written from inside the repo; the first segment is forgiven
		"other/shared/deep/x.ts": true,
	} {
		if inWorktree.Allows(rel) != want {
			t.Errorf("%s: allowed=%v, want %v", rel, inWorktree.Allows(rel), want)
		}
	}
	if got := InRepo("/ws", "/ws/api", "/ws/api/limits/h.go"); got != "api/limits/h.go" {
		t.Errorf("in a sub-repo: %s", got)
	}
	if got := InRepo("/ws", "/ws/.corgi/corgi_services/.worktrees/api-3f2a1b@feature-x", "/ws/.corgi/corgi_services/.worktrees/api-3f2a1b@feature-x/limits/h.go"); got != "api/limits/h.go" {
		t.Errorf("in a worktree: %s", got)
	}
	if got := InRepo("/ws", "/ws", "/ws/api/limits/h.go"); got != "api/limits/h.go" {
		t.Errorf("in a monorepo: %s", got)
	}
	if (Scope{}).Allows("anything/at/all.go") != true {
		t.Fatal("no paths means anywhere")
	}
	if err := Remove(dir, "ABC-1"); err != nil {
		t.Fatal(err)
	}
}

func TestTestFilesAreRecognisedAcrossStacks(t *testing.T) {
	for rel, want := range map[string]bool{
		"api/limits_test.go":       true,
		"web/src/Banner.test.tsx":  true,
		"web/src/__tests__/x.ts":   true,
		"svc/tests/test_limits.py": true,
		"svc/spec/limits_spec.rb":  true,
		"api/limits.go":            false,
		"web/src/Banner.tsx":       false,
		"docs/testing.md":          false,
	} {
		if IsTestFile(rel) != want {
			t.Errorf("%s: %v, want %v", rel, IsTestFile(rel), want)
		}
	}
}
