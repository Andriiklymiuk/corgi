package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestWatchStatusSaysWhatIsStillMissing(t *testing.T) {
	_, ws := watchFixture(t, nil)
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("PATH", "")

	got, err := mcpWatchStatus(watchStatusArgs{})
	if err != nil {
		t.Fatal(err)
	}
	rows := got["workspaces"].([]watchWorkspaceStatus)
	if len(rows) != 1 || rows[0].Workspace != "acme-stack" || rows[0].Dir != ws {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Enabled {
		t.Error("a workspace with no watch config is not enabled")
	}
	joined := strings.Join(rows[0].Suggests, " | ")
	for _, want := range []string{"watch is off here", "no tracker token"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	for _, s := range rows[0].Sources {
		if s.HasToken {
			t.Errorf("%s should have no token: %+v", s.Name, s)
		}
		if s.TokenLabel != "none" {
			t.Errorf("a missing token prints as none, got %q", s.TokenLabel)
		}
	}

	if _, err := mcpWatchStatus(watchStatusArgs{Workspace: "nope"}); err == nil {
		t.Error("an unknown workspace is an error, not an empty list")
	}
}

func TestWatchEnableWritesTheConfigAndReportsTheGaps(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	t.Setenv("LINEAR_API_KEY", "lin_x")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")

	yes := true
	got, err := mcpWatchEnable(watchEnableArgs{
		Workspace: "acme-stack", Tracker: "linear", Project: "ABC",
		Repos: []string{"acme/web", " acme/api ", ""}, States: []string{"In Progress"}, PRs: &yes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["workspace"] != "acme-stack" {
		t.Fatalf("%+v", got)
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	wc := user.Workspaces["acme-stack"].Watch
	if wc == nil || !wc.Enabled || wc.Tracker != "linear" || wc.Project != "ABC" || !wc.PRs {
		t.Fatalf("watch = %+v", wc)
	}
	if len(wc.Repos) != 2 || wc.Repos[0] != "acme/api" || wc.Repos[1] != "acme/web" {
		t.Errorf("blanks dropped and sorted, got %v", wc.Repos)
	}
	if gaps, _ := got["whatIsMissing"].([]string); len(gaps) != 0 {
		t.Errorf("a linear token and a project leave nothing missing, got %v", gaps)
	}

	// A workspace with prs on and no repos cannot route a review.
	if _, err := mcpWatchEnable(watchEnableArgs{Workspace: "acme-stack", Repos: nil}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINEAR_API_KEY", "")
	status, _ := mcpWatchStatus(watchStatusArgs{Workspace: "acme-stack"})
	rows := status["workspaces"].([]watchWorkspaceStatus)
	if !strings.Contains(strings.Join(rows[0].Suggests, " | "), "no tracker token") {
		t.Errorf("a removed token should show up: %v", rows[0].Suggests)
	}

	for _, bad := range []watchEnableArgs{
		{Workspace: "acme-stack", Tracker: "notion"},
		{Workspace: "acme-stack", Action: "merge"},
		{Workspace: "nope"},
	} {
		if _, err := mcpWatchEnable(bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

func TestWatchEventsForMCPFilterAndFlag(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(
		`{"key":"a","kind":"issue.new","ref":"ABC-1","title":"one\ntwo","workspace":"acme-stack","at":"2026-09-10T10:00:00Z"}`+"\n"+
			`{"key":"b","kind":"pr.review","ref":"acme/api#3","workspace":"other","at":"2026-09-10T11:00:00Z"}`+"\n"), 0o600)

	all, err := watchEventsForMCP("", 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v, %v", all, err)
	}
	if all[0]["key"] != "b" {
		t.Errorf("newest first, got %v", all[0]["key"])
	}
	if all[0]["canWorkOn"] != true || all[1]["canWorkOn"] != true {
		t.Error("both kinds have a skill behind them")
	}
	mine, _ := watchEventsForMCP("acme-stack", 25)
	if len(mine) != 1 || mine[0]["title"] != "one" {
		t.Fatalf("filtered = %v", mine)
	}
}
