package watch

import (
	"strings"
	"testing"
	"time"
)

func TestTheBoardsWorkHoldsWhatIsLeftTheStalledAndTheWeek(t *testing.T) {
	now := time.Now()
	link := "https://github.com/acme/api/pull/1"
	recs := []FixRecord{{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: string(KindIssueNew), StartedAt: now.Add(-time.Hour), FinishedAt: now, CostUSD: 1.5, PRs: []string{link},
		Left: []LeftItem{{Text: "Run the migration on staging.", Kind: LeftOther}}, Gaps: []Gap{GapNoDatabase}}}
	pulls := map[string]PullStatus{"acme/api#1": {State: "open", Checks: "passing", Review: "approved", Mine: true, Link: link, Workspace: "acme", Since: now.Add(-30 * time.Hour)}}
	w := BoardWork(recs, nil, pulls, now)
	if len(w.Left) != 1 || w.Left[0].ID != "k1#0" || w.Left[0].URL != link {
		t.Fatalf("left: %+v", w.Left)
	}
	if len(w.Stalled) != 1 || w.Stalled[0].Why != "approved, checks ✓, not merged for a day" {
		t.Fatalf("stalled: %+v", w.Stalled)
	}
	if w.Spend == nil || w.Spend.SkippedChecks != 1 || w.Check == nil || !strings.Contains(w.Check.Headline, "1 unattended runs in 7 days") {
		t.Fatalf("spend and check: %+v %+v", w.Spend, w.Check)
	}
	if empty := BoardWork(nil, nil, nil, now); empty.Spend != nil || empty.Check != nil || empty.Left != nil {
		t.Fatalf("a laptop with no runs has nothing to say: %+v", empty)
	}
}

func TestSelfCheckWithNothingWrongSaysSo(t *testing.T) {
	now := time.Now()
	c := SelfCheckOf([]FixRecord{{Key: "1", Kind: string(KindIssueNew), StartedAt: now, FinishedAt: now, Said: "pushed"}}, nil, now, 7)
	if len(c.Lines) != 0 || c.Headline() != "1 unattended runs in 7 days, nothing kept going wrong" {
		t.Fatalf("%q %v", c.Headline(), c.Lines)
	}
	one := SelfCheck{Runs: 3, Days: 7, Lines: []string{"x"}}
	if one.Headline() != "3 unattended runs in 7 days: 1 thing worth a look" {
		t.Fatalf("%q", one.Headline())
	}
	if got := sortedGaps(map[Gap]int{GapNoTool: 1, GapNoDatabase: 3, GapNoDevice: 1}); got[0] != GapNoDatabase || got[1] != GapNoDevice {
		t.Fatalf("most first, then by name: %v", got)
	}
	if runsWord(1) != "run" || itemsWord(2) != "items" {
		t.Fatal("plural words")
	}
}

func TestSmallPiecesOfWhatIsLeft(t *testing.T) {
	if !strings.Contains(LeftInstruction(), "## Left for you") {
		t.Fatal("the run is told the heading")
	}
	if LastRunOf("no header here") != "no header here" {
		t.Fatal("a log without a run header is one run")
	}
	long := newLeft(strings.Repeat("a", 300))
	if len(long.Text) > leftClip+2 || !strings.HasSuffix(long.Text, "…") {
		t.Fatalf("a long item is clipped: %d", len(long.Text))
	}
	if daysWord(30*time.Hour) != "a day" || daysWord(72*time.Hour) != "3 days" {
		t.Fatal("days")
	}
	l := LoadFixLog(t.TempDir())
	l.StartFor(Event{Key: "k", Workspace: "a"}, time.Now())
	l.SetGaps("k", []Gap{GapNoDevice})
	l.SetGaps("", []Gap{GapNoDevice})
	if got := l.Records()[0].Gaps; len(got) != 1 {
		t.Fatalf("gaps stored: %v", got)
	}
	p := LoadPullLog(t.TempDir())
	_ = p.Set("a/b#1", PullStatus{State: "open"})
	if all := p.All(); len(all) != 1 || all["a/b#1"].State != "open" {
		t.Fatalf("all: %v", all)
	}
}
