package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestCostByRepoDayAndBot(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.Local)
	// The ledger: two days, two workspaces.
	l := usage.OpenLedger(dir)
	l.AddTokens("api", 1_000_000, now)
	l.AddTokens("web", 250_000, now)
	l.AddTokens("api", 500_000, now.AddDate(0, 0, -1))
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	// The fix log: a bot's run with a receipt, and a plain run.
	fixes := watch.LoadFixLog(dir)
	e1 := watch.Event{Key: "k1", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueNew}
	fixes.StartFor(e1, now.Add(-time.Hour))
	fixes.SetCost("k1", 0.42, 120_000)
	fixes.Finish("k1", nil, "", "", now)
	e2 := watch.Event{Key: "bot:reviewer:k2", Ref: "ABC-2", Workspace: "web", Kind: watch.KindPRReview}
	fixes.StartFor(e2, now.Add(-2*time.Hour))
	fixes.SetBot("bot:reviewer:k2", "reviewer")
	fixes.SetCost("bot:reviewer:k2", 0.10, 30_000)
	_ = os.WriteFile(filepath.Join(dir, "config.yml"), []byte("workspaces:\n  api:\n    watch:\n      dayCap: 2000000\n"), 0o600)

	rows, err := costBy(dir, "repo", 14, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Key != "api" || rows[0].Tokens != 1_620_000 || rows[0].USD != 0.42 || rows[0].Runs != 1 || rows[0].DayCap != 2_000_000 {
		t.Fatalf("repo: %+v", rows)
	}
	if rows[1].Key != "web" || rows[1].Tokens != 280_000 || rows[1].Runs != 1 {
		t.Fatalf("web: %+v", rows[1])
	}
	days, _ := costBy(dir, "day", 3, now)
	if len(days) != 3 || days[2].Key != usage.Today(now) || days[2].Tokens != 1_400_000 || days[1].Tokens != 500_000 || days[0].Tokens != 0 {
		t.Fatalf("day: %+v", days)
	}
	bots, _ := costBy(dir, "bot", 14, now)
	if len(bots) != 1 || bots[0].Key != "reviewer" || bots[0].Tokens != 30_000 || bots[0].USD != 0.10 {
		t.Fatalf("bot: %+v", bots)
	}
	if _, err := costBy(dir, "moon", 14, now); err == nil {
		t.Fatal("by what?")
	}
}
