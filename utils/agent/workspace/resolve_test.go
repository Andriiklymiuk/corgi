package workspace

import (
	"strings"
	"testing"
)

func testRegistry() *Registry {
	return &Registry{Workspaces: []Workspace{
		{
			ID:       "acme-stack",
			Aliases:  []string{"acme", "todo app"},
			AbsPath:  "/home/dev/acme",
			Repos:    []string{"api", "web", "mobile"},
			Services: []string{"api", "web", "db"},
		},
		{
			ID:       "side-project",
			Aliases:  []string{"side"},
			AbsPath:  "/home/dev/sideproject",
			Repos:    []string{"backend"},
			Services: []string{"backend", "cache"},
		},
	}}
}

func TestResolveExactID(t *testing.T) {
	got := Resolve(testRegistry(), "acme-stack")

	if !got.Resolved() {
		t.Fatalf("expected a resolution, got candidates: %v", got.Reason)
	}
	if got.Workspace.ID != "acme-stack" {
		t.Errorf("resolved %q, want acme-stack", got.Workspace.ID)
	}
	if got.MatchedOn != MatchID {
		t.Errorf("matched on %q, want %q", got.MatchedOn, MatchID)
	}
}

func TestResolveAliasWithDifferentPunctuation(t *testing.T) {
	for _, query := range []string{"todo app", "todo-app", "Todo App", "  TODO_APP  "} {
		got := Resolve(testRegistry(), query)
		if !got.Resolved() {
			t.Errorf("query %q did not resolve: %s", query, got.Reason)
			continue
		}
		if got.Workspace.ID != "acme-stack" {
			t.Errorf("query %q resolved to %q, want acme-stack", query, got.Workspace.ID)
		}
	}
}

func TestResolveByServiceName(t *testing.T) {
	got := Resolve(testRegistry(), "fix the api")

	if !got.Resolved() {
		t.Fatalf("expected the stack that has a service called api, got: %s", got.Reason)
	}
	if got.Workspace.ID != "acme-stack" {
		t.Errorf("resolved %q, want acme-stack", got.Workspace.ID)
	}
}

func TestResolveByDirectoryName(t *testing.T) {
	got := Resolve(testRegistry(), "sideproject")

	if !got.Resolved() || got.Workspace.ID != "side-project" {
		t.Fatalf("expected side-project via its directory, got %+v (%s)", got.Workspace, got.Reason)
	}
}

func TestResolveNeverGuessesWhenAmbiguous(t *testing.T) {
	r := &Registry{Workspaces: []Workspace{
		{ID: "acme-api", AbsPath: "/a", Services: []string{"api"}},
		{ID: "beta-api", AbsPath: "/b", Services: []string{"api"}},
	}}

	got := Resolve(r, "api")

	if got.Resolved() {
		t.Fatal("an ambiguous query must not resolve: a wrong resolution means an agent editing the wrong repository")
	}
	if len(got.Candidates) != 2 {
		t.Errorf("got %d candidates, want 2", len(got.Candidates))
	}
	if !strings.Contains(got.Reason, "pick one") {
		t.Errorf("reason %q should ask the user to pick", got.Reason)
	}
}

func TestResolveDuplicateAliasIsAmbiguousNotFirstWins(t *testing.T) {
	r := &Registry{Workspaces: []Workspace{
		{ID: "one", Aliases: []string{"shared"}, AbsPath: "/one"},
		{ID: "two", Aliases: []string{"shared"}, AbsPath: "/two"},
	}}

	got := Resolve(r, "shared")

	if got.Resolved() {
		t.Fatal("two workspaces sharing an alias must be reported as ambiguous, not silently first-wins")
	}
	if len(got.Candidates) != 2 {
		t.Errorf("got %d candidates, want 2", len(got.Candidates))
	}
}

func TestResolveExactAliasBeatsFuzzyMatchElsewhere(t *testing.T) {
	r := &Registry{Workspaces: []Workspace{
		{ID: "exactly-web", Aliases: []string{"web"}, AbsPath: "/one"},
		{ID: "other", AbsPath: "/two", Services: []string{"web-frontend", "webhooks"}},
	}}

	got := Resolve(r, "web")

	if !got.Resolved() {
		t.Fatalf("an exact alias should win outright, got: %s", got.Reason)
	}
	if got.Workspace.ID != "exactly-web" {
		t.Errorf("resolved %q, want exactly-web", got.Workspace.ID)
	}
}

func TestResolveNoMatchOffersEverything(t *testing.T) {
	got := Resolve(testRegistry(), "something that does not exist")

	if got.Resolved() {
		t.Fatal("an unmatched query must not resolve")
	}
	if len(got.Candidates) != 2 {
		t.Errorf("got %d candidates, want all 2 so the user can pick", len(got.Candidates))
	}
	if !strings.Contains(got.Reason, "no workspace matched") {
		t.Errorf("reason %q should say nothing matched", got.Reason)
	}
}

func TestResolveEmptyQueryAsksRatherThanPicking(t *testing.T) {
	got := Resolve(testRegistry(), "   ")

	if got.Resolved() {
		t.Fatal("an empty query must not resolve to whatever happens to be first")
	}
	if len(got.Candidates) == 0 {
		t.Error("an empty query should offer the known workspaces")
	}
}

func TestResolveEmptyRegistry(t *testing.T) {
	got := Resolve(&Registry{}, "anything")

	if got.Resolved() {
		t.Fatal("an empty registry cannot resolve anything")
	}
	if len(got.Candidates) != 0 {
		t.Error("an empty registry has no candidates to offer")
	}
}

func TestResolutionReasonEchoesPathForConfirmation(t *testing.T) {
	got := Resolve(testRegistry(), "acme-stack")

	if !strings.Contains(got.Reason, "/home/dev/acme") {
		t.Errorf("reason %q must include the resolved path so a mis-resolution is caught before any code is written", got.Reason)
	}
}

func TestRelatedIgnoresVeryShortWords(t *testing.T) {
	r := &Registry{Workspaces: []Workspace{{ID: "alpha", AbsPath: "/a", Services: []string{"db"}}}}

	// "do" should not reach the "db" service through word overlap.
	if got := Resolve(r, "do the thing"); got.Resolved() {
		t.Errorf("short incidental words must not resolve a workspace, got %q", got.Workspace.ID)
	}
}
