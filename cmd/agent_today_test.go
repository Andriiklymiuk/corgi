package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func todayAgentDir(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "watch"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTodaySinceIsMidnightUnlessAsked(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.Local)
	got, err := todaySince(agentTodayCmd, now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("today starts at midnight, not %s", got)
	}

	if err := agentTodayCmd.Flags().Set("since", "8h"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agentTodayCmd.Flags().Set("since", "") }()
	got, err = todaySince(agentTodayCmd, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(now.Add(-8 * time.Hour)) {
		t.Fatalf("--since asks for a rolling window, got %s", got)
	}

	if err := agentTodayCmd.Flags().Set("since", "yesterday"); err != nil {
		t.Fatal(err)
	}
	if _, err := todaySince(agentTodayCmd, now); err == nil {
		t.Fatal("a window that is not a duration is an error, not midnight")
	}
}

func TestTodayCountsAndHeadline(t *testing.T) {
	entries := []standupEntry{{
		Workspace: "api",
		Commits:   []string{"one", "two"},
		Prompts:   []string{"do the thing"},
		Fixes: []standupFix{
			{Ref: "ABC-1", Outcome: "opened 1 PR", PRs: []string{"https://x/pull/1"}},
			{Ref: "ABC-2", Running: true, Outcome: "running"},
		},
		Arrived:  []standupEvent{{Ref: "ABC-3"}},
		Deferred: []string{"ABC-4"},
	}}
	got := countToday(entries)
	want := todayTotals{Workspaces: 1, Commits: 2, Prompts: 1, Fixes: 2, Running: 1, PRs: 1, Arrived: 1, Deferred: 1}
	if got != want {
		t.Fatalf("totals: got %+v want %+v", got, want)
	}
	line := got.headline()
	for _, want := range []string{"2 commits", "1 pull request from the watch", "1 still running", "1 waiting"} {
		if !strings.Contains(line, want) {
			t.Fatalf("headline %q is missing %q", line, want)
		}
	}
	if empty := (todayTotals{}).headline(); empty != "nothing yet" {
		t.Fatalf("a quiet day says so: %q", empty)
	}
}

func TestTodayIncludesWhatTheWatchDidUnattended(t *testing.T) {
	dir := todayAgentDir(t)
	now := time.Now()
	log := watch.LoadFixLog(dir)
	log.StartFor(watch.Event{Key: "jira:ABC-1", Workspace: "api", Ref: "ABC-1", Kind: watch.KindIssueNew, Title: "Login loops"}, now.Add(-90*time.Minute))
	log.Finish("jira:ABC-1", []string{"https://git/x/-/merge_requests/7"}, "", "", now.Add(-70*time.Minute))
	log.StartFor(watch.Event{Key: "jira:ABC-2", Workspace: "api", Ref: "ABC-2", Title: "Still going"}, now.Add(-5*time.Minute))
	log.StartFor(watch.Event{Key: "jira:OLD-9", Workspace: "api", Ref: "OLD-9"}, now.Add(-72*time.Hour))
	log.Finish("jira:OLD-9", nil, "", "timed out", now.Add(-71*time.Hour))
	log.Defer(watch.Event{Key: "jira:ABC-8", Workspace: "api", Ref: "ABC-8"})

	events := []watch.Event{
		{Key: "jira:ABC-3", Workspace: "api", Source: "jira", Ref: "ABC-3", Title: "New ticket", At: now.Add(-30 * time.Minute)},
		{Key: "jira:ABC-0", Workspace: "api", Source: "jira", Ref: "ABC-0", Title: "Last week", At: now.Add(-200 * time.Hour)},
	}
	var lines []string
	for _, e := range events {
		row, _ := json.Marshal(e)
		lines = append(lines, string(row))
	}
	if err := os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	byKey := map[string]*standupEntry{}
	entryFor := func(d string) *standupEntry {
		e := byKey[d]
		if e == nil {
			e = &standupEntry{Workspace: filepath.Base(d), Dir: d}
			byKey[d] = e
		}
		return e
	}
	addWatchActions(now.Add(-24*time.Hour), byKey, entryFor)

	e := byKey["api"]
	if e == nil {
		t.Fatalf("a workspace with only unattended work still gets a row: %v", byKey)
	}
	if len(e.Fixes) != 2 {
		t.Fatalf("yesterday's run is out of the window: %+v", e.Fixes)
	}
	if e.Fixes[0].Ref != "ABC-1" || e.Fixes[0].Outcome != "opened 1 PR" {
		t.Fatalf("oldest first, with what it opened: %+v", e.Fixes[0])
	}
	if !e.Fixes[1].Running || e.Fixes[1].Outcome != "running" {
		t.Fatalf("a run with no outcome is still running: %+v", e.Fixes[1])
	}
	if len(e.Arrived) != 1 || e.Arrived[0].Ref != "ABC-3" {
		t.Fatalf("only what arrived in the window: %+v", e.Arrived)
	}
	if len(e.Deferred) != 1 || e.Deferred[0] != "ABC-8" {
		t.Fatalf("what a cap held back is worth saying: %+v", e.Deferred)
	}

	var b strings.Builder
	writeWatchActions(&b, *e)
	text := b.String()
	for _, want := range []string{"corgi worked on 2 things unattended", "ABC-1 — opened 1 PR", "merge_requests/7", "… ABC-2", "the watch saw 1 thing", "waiting for a free slot: ABC-8"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the report is missing %q:\n%s", want, text)
		}
	}
}

func TestWatchActionsSurviveNoAgentDataAndCapTheArrivals(t *testing.T) {
	dir := todayAgentDir(t)
	byKey := map[string]*standupEntry{}
	entryFor := func(d string) *standupEntry { return &standupEntry{Dir: d} }
	addWatchActions(time.Now().Add(-24*time.Hour), byKey, entryFor)
	if len(byKey) != 0 {
		t.Fatalf("no watch data is no rows, not a crash: %v", byKey)
	}

	now := time.Now()
	var lines []string
	for i := 0; i < arrivedCap+5; i++ {
		row, _ := json.Marshal(watch.Event{Key: "k" + string(rune('a'+i)), Workspace: "api", Ref: "R", At: now})
		lines = append(lines, string(row))
	}
	if err := os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addWatchActions(now.Add(-time.Hour), byKey, entryFor)
	if got := len(byKey["api"].Arrived); got != arrivedCap {
		t.Fatalf("a busy tracker must not bury the day's own work: %d", got)
	}
}

func TestFixOutcomeReadsTheSameEverywhere(t *testing.T) {
	done := time.Now()
	cases := []struct {
		name string
		r    watch.FixRecord
		want string
	}{
		{"running", watch.FixRecord{}, "running"},
		{"failed", watch.FixRecord{FinishedAt: done, Error: "timed out"}, "timed out"},
		{"one pr", watch.FixRecord{FinishedAt: done, PRs: []string{"a"}}, "opened 1 PR"},
		{"two prs", watch.FixRecord{FinishedAt: done, PRs: []string{"a", "b"}}, "opened 2 PRs"},
		{"note", watch.FixRecord{FinishedAt: done, Note: "no change needed"}, "no change needed"},
		{"quiet", watch.FixRecord{FinishedAt: done}, "nothing opened"},
	}
	for _, c := range cases {
		if got := c.r.Outcome(); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
