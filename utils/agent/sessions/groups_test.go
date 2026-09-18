package sessions

import "testing"

func TestGroupsCollectSessionsByTicketThenBranch(t *testing.T) {
	sessions := []Session{
		{ID: "a", Label: "api", TicketKey: "abc-12", Ticket: "https://tracker/ABC-12", Branch: "feature/abc-12-limits", PR: "https://forge/api/pull/1", Status: StatusWorking, Home: "/w/api", Cwd: "/w/api/.corgi/worktrees/abc-12"},
		{ID: "b", Label: "web", Branch: "feature/ABC-12-banner", Status: StatusNeedsInput, Home: "/w/web", Cwd: "/w/web"},
		{ID: "c", Label: "api", Branch: "main"},
		{ID: "d", Label: "web", Branch: "spike/dark-mode", Attempt: "2"},
		{ID: "e", Label: "api", Branch: "feature/abc-12-limits", PR: "https://forge/api/pull/1"},
	}
	groups := Groups(sessions)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %+v", groups)
	}
	abc := groups[0]
	if abc.Key != "ABC-12" || abc.Ticket != "https://tracker/ABC-12" || len(abc.Sessions) != 3 {
		t.Fatalf("ticket group: %+v", abc)
	}
	if len(abc.Workspaces) != 2 || len(abc.Branches) != 2 || len(abc.PRs) != 1 {
		t.Fatalf("deduped lists: %+v", abc)
	}
	if len(abc.Worktrees) != 1 || abc.Worktrees[0] != "/w/api/.corgi/worktrees/abc-12" {
		t.Fatalf("only a cwd away from home is a worktree: %v", abc.Worktrees)
	}
	if abc.Working != 1 || abc.NeedsInput != 1 {
		t.Fatalf("counts: %+v", abc)
	}
	spike := groups[1]
	if spike.Key != "spike/dark-mode" || spike.Attempts != 1 || len(spike.Sessions) != 1 {
		t.Fatalf("branch group: %+v", spike)
	}
}

func TestGroupKeySkipsTrunkBranches(t *testing.T) {
	for _, b := range []string{"", "main", "master", "develop", "HEAD"} {
		if k := GroupKey(Session{Branch: b}); k != "" {
			t.Fatalf("%q → %q", b, k)
		}
	}
	if k := GroupKey(Session{Branch: "main", TicketKey: "xy-1"}); k != "XY-1" {
		t.Fatalf("ticket key wins: %q", k)
	}
}
