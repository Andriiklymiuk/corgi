package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestARunIsAskedForWhatItLeavesAndTheForgeClosesIt(t *testing.T) {
	if s := unattendedSuffix(WatchSpec{Workspace: "acme"}, watch.Event{Kind: watch.KindIssueNew, Ref: "ABC-1"}); !strings.Contains(s, "## Left for you") {
		t.Fatalf("the run is told where to put what is left for a person:\n%s", s)
	}
	d := testDaemon(t)
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir}
	link := "https://github.com/acme/api/pull/7"
	log := watch.LoadFixLog(d.Dir)
	now := time.Now()
	log.StartFor(watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: watch.KindIssueNew}, now)
	log.Finish("k1", []string{link}, "", "", now)
	log.SetLeft("k1", watch.LeftFor("## Left for you\n- Wait for CI on the new commit.\n- Merge it after web #4.\n"))

	pending := watch.PullStatus{State: "open", Checks: "pending", Mine: true, At: now}
	green := watch.PullStatus{State: "open", Checks: "passing", Mine: true, At: now}
	d.pullChanged(context.Background(), spec, "acme/api#7", link, pending, green, true)
	open := watch.OpenLeft(watch.LeftRows(watch.LoadFixLog(d.Dir).Records(), nil, now.Add(-time.Hour), now))
	if len(open) != 1 || open[0].Kind != watch.LeftMerge {
		t.Fatalf("green checks closed the CI item: %+v", open)
	}
	d.pullChanged(context.Background(), spec, "acme/api#7", link, green, watch.PullStatus{State: "merged", Checks: "passing", Mine: true, At: now}, true)
	if open := watch.OpenLeft(watch.LeftRows(watch.LoadFixLog(d.Dir).Records(), nil, now.Add(-time.Hour), now)); len(open) != 0 {
		t.Fatalf("the merge closed the rest: %+v", open)
	}
}

func TestTheBoardCarriesWhatIsLeftTheSpendAndTheStalledPullRequests(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	now := time.Now()
	log := watch.LoadFixLog(d.Dir)
	log.StartFor(watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: watch.KindIssueNew}, now)
	log.Finish("k1", []string{"https://github.com/acme/api/pull/1"}, "", "", now)
	log.SetCost("k1", 2.5, 100)
	log.SetLeft("k1", watch.LeftFor("## Left for you\n- Run the migration on staging.\n"))
	_ = watch.LoadPullLog(d.Dir).Set("acme/api#1", watch.PullStatus{State: "open", Checks: "passing", Review: "approved", Mine: true,
		Link: "https://github.com/acme/api/pull/1", Workspace: "acme", Since: now.Add(-30 * time.Hour), At: now})
	d.sampleWork(now)
	board := d.Sessions.Snapshot(now)
	if len(board.Left) != 1 || board.Left[0].Text != "Run the migration on staging." {
		t.Fatalf("left on the board: %+v", board.Left)
	}
	if len(board.Stalled) != 1 || board.Stalled[0].Action != "merge" {
		t.Fatalf("stalled on the board: %+v", board.Stalled)
	}
	if board.Spend == nil || board.Spend.WeekUSD != 2.5 || board.Spend.WeekRuns != 1 {
		t.Fatalf("spend on the board: %+v", board.Spend)
	}
}

func TestForgeCallsInARunEndAfterTheLimit(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho gh \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	agentDir := t.TempDir()
	dir := writeForgeShims(agentDir)
	if dir == "" {
		t.Fatal("no shims written")
	}
	out, err := exec.Command(filepath.Join(dir, "gh"), "pr", "view", "it's 7").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "gh pr view it's 7" {
		t.Fatalf("the wrapper runs the real tool with its arguments: %q %v", out, err)
	}
	script, _ := os.ReadFile(filepath.Join(dir, "gh"))
	if !strings.Contains(string(script), "alarm") || !strings.Contains(string(script), "120") {
		t.Fatalf("the wrapper ends the call after the limit:\n%s", script)
	}
	env := withForgeShims(agentDir, nil)
	if len(env) != 1 || !strings.HasPrefix(env[0], "PATH="+dir+string(os.PathListSeparator)) {
		t.Fatalf("the run finds the wrappers first: %v", env)
	}
}

func TestSelfCheckRingsOnceAWeekOnMondayMorning(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	d.Watches = []WatchSpec{{Workspace: "acme"}}
	d.watchState = watch.LoadState(d.Dir)
	notes := make(chan string, 4)
	d.Notify = func(_, body string) { notes <- body }
	d.NotifyWithLink = func(title, body, _ string) { d.Notify(title, body) }
	monday := time.Date(2026, 10, 26, 9, 30, 0, 0, time.Local)
	d.watchState.Fixes.StartFor(watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: watch.KindIssueNew}, monday.Add(-time.Hour))
	d.watchState.Fixes.Finish("k1", nil, "", "", monday.Add(-time.Hour))
	d.watchState.Fixes.SetGaps("k1", []watch.Gap{watch.GapNoDatabase})
	d.weeklySelfCheck(monday.Add(-24 * time.Hour))
	d.weeklySelfCheck(monday)
	d.weeklySelfCheck(monday.Add(time.Hour))
	select {
	case body := <-notes:
		if !strings.Contains(body, "no database") {
			t.Fatalf("the self-check names the gap: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no self-check on Monday morning")
	}
	select {
	case body := <-notes:
		t.Fatalf("rang twice: %q", body)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestEarlierRunsAreReadBackFromTheirLogs(t *testing.T) {
	d := testDaemon(t)
	d.Watches = []WatchSpec{{Workspace: "acme"}}
	d.loadWatchFiles()
	now := time.Now()
	d.watchState.Fixes.StartFor(watch.Event{Key: "github:acme/api#9", Workspace: "acme", Ref: "acme/api#9", Kind: watch.KindPRReview}, now)
	d.watchState.Fixes.Finish("github:acme/api#9", nil, "", "", now)
	runs := filepath.Join(d.Dir, "watch", "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	log := "=== 2026-10-22T10:00:00+02:00 pr.review acme/api#9\nThe SQL did not run: no Postgres here.\n## What you do next\n- Run the SQL against a local database.\n"
	if err := os.WriteFile(filepath.Join(runs, safeName("github:acme/api#9")+".log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	d.backfillRunNotes()
	r := d.watchState.Fixes.Records()[0]
	if len(r.Left) != 1 || len(r.Gaps) != 1 || r.Gaps[0] != watch.GapNoDatabase {
		t.Fatalf("read back: %+v %+v", r.Left, r.Gaps)
	}
	(&Daemon{}).backfillRunNotes()
}
