package watch

import (
	"strings"
	"testing"
	"time"
)

func TestLeftForReadsTheListARunLeavesForAPerson(t *testing.T) {
	out := "Outcome: pushed\n\nI fixed the null check.\n\n## Left for you\n\n- Run the migration on staging.\n- Wait for CI to run on the new commit.\n- **Merge** api #12 before web #4.\n- I did not wait for CI.\n\n=== cost: $0.10 · 1 tokens · 1 turns\n"
	got := LeftFor(out)
	if len(got) != 3 {
		t.Fatalf("three things are left, the agent's own statement is not one: %+v", got)
	}
	want := []LeftKind{LeftOther, LeftCI, LeftMerge}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("item %d %q: kind %s, want %s", i, got[i].Text, got[i].Kind, k)
		}
	}
	if strings.Contains(got[2].Text, "**") {
		t.Errorf("markdown emphasis stays out of the text: %q", got[2].Text)
	}
}

func TestLeftForReadsTheOlderHeadingsAndSaysNothingWhenNothingIsLeft(t *testing.T) {
	if got := LeftFor("## What you do next\n- Confirm who owns the Google Cloud project.\n"); len(got) != 1 || got[0].Kind != LeftOther {
		t.Fatalf("an older heading still counts: %+v", got)
	}
	if got := LeftFor("**What you do next**\n- Nothing. The PR is approved.\n"); len(got) != 0 {
		t.Fatalf("nothing left is no item: %+v", got)
	}
	if got := LeftFor("## What you do next\n\nNothing for CI.\n"); len(got) != 0 {
		t.Fatalf("a sentence saying nothing is left is no item: %+v", got)
	}
	if got := LeftFor("- a list with no heading\n"); got != nil {
		t.Fatalf("no heading, no items: %+v", got)
	}
	if got := LeftFor("## Left for you\n- docs #51 (Ci.md) should be closed.\n"); got[0].Kind != LeftOther {
		t.Fatalf("a file called Ci.md is not CI: %+v", got)
	}
}

func TestTickLeftClosesTheItemsTheForgeSawHappen(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	link := "https://github.com/acme/api/pull/7"
	l.StartFor(Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: KindIssueNew}, now)
	l.Finish("k1", []string{link}, "", "", now)
	l.SetLeft("k1", LeftFor("## Left for you\n- Wait for CI on the new commit.\n- Run the migration on staging.\n- Merge it after web.\n"))

	if n := l.TickLeft("acme", link, []LeftKind{LeftCI}, "checks ✓", now); n != 1 {
		t.Fatalf("green checks close the CI item only: %d", n)
	}
	if n := l.TickLeft("other", link, []LeftKind{LeftMerge}, "merged", now); n != 0 {
		t.Fatalf("another workspace's pull request closes nothing here: %d", n)
	}
	rows := LeftRows(l.Records(), map[string]LeftTick{"k1#1": {At: now, By: "you"}}, now.Add(-time.Hour), now)
	open := OpenLeft(rows)
	if len(rows) != 3 || len(open) != 1 || open[0].Kind != LeftMerge {
		t.Fatalf("the CI item closed itself, the person ticked the migration, the merge waits: %+v", rows)
	}
	if rows[0].URL != link {
		t.Errorf("a row links to the pull request the run opened: %q", rows[0].URL)
	}
}

func TestBackfillReadsTheLastRunOfALogAndClosesWhatAlreadyMerged(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	link := "https://github.com/acme/api/pull/9"
	l.StartFor(Event{Key: "k2", Workspace: "acme", Ref: "acme/api#9", URL: link, Kind: KindPRReview}, now)
	l.Finish("k2", nil, "", "", now)
	log := "=== 2026-10-01T10:00:00+02:00 pr.review acme/api#9\n## What you do next\n- Old item from the first run.\n" +
		"=== 2026-10-02T10:00:00+02:00 pr.review acme/api#9\nNo Postgres here, so the SQL did not run.\n## What you do next\n- Check that CI goes green.\n- Run the SQL against a local database.\n"
	pulls := map[string]PullStatus{"acme/api#9": {State: "merged", At: now}}
	if n := l.Backfill(func(string) string { return log }, pulls, now); n != 1 {
		t.Fatalf("one run filled: %d", n)
	}
	r := l.Records()[0]
	if len(r.Left) != 2 || r.Left[0].DoneBy != "merged" || !r.Left[1].DoneAt.IsZero() {
		t.Fatalf("the last run's items, the CI one closed by the merge: %+v", r.Left)
	}
	if len(r.Gaps) != 1 || r.Gaps[0] != GapNoDatabase {
		t.Fatalf("the skipped check is read: %+v", r.Gaps)
	}
}

func TestLeftTicksLiveInAFileOfTheirOwn(t *testing.T) {
	dir := t.TempDir()
	if err := LoadLeftTicks(dir).Tick("k1#0", "phone", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := LoadLeftTicks(dir).All()["k1#0"]; got.By != "phone" {
		t.Fatalf("a tick survives a reload: %+v", got)
	}
}

func TestAReviewRunGetsAnOutcomeFromWhatItPosted(t *testing.T) {
	cases := map[string]string{
		"Outcome: pushed\nfixed it":                                     "pushed",
		"acme/web!80: comments (2: 1 summary, 1 inline), not approved.": "replied",
		"acme/api#5: approved":                                          "replied",
		"- acme/api#5 - 3 inline comments":                              "replied",
		"acme/api#5: nothing":                                           "nothing",
		"I looked at the diff.":                                         "",
	}
	for out, want := range cases {
		if got := OutcomeOf(KindReviewRequested, out); got != want {
			t.Errorf("review run %q: %q, want %q", out, got, want)
		}
	}
	if got := OutcomeOf(KindPRComment, "acme/api#5: approved"); got != "" {
		t.Fatalf("only a review run is read off what it posted: %q", got)
	}

	if got := LeftFor("## What you do next\n- Merge when you are ready. I never merge.\n- Decide who owns the follow-up.\n"); len(got) != 1 || !strings.HasPrefix(got[0].Text, "Decide") {
		t.Fatalf("\"merge when you are ready\" is not a thing left for a person: %+v", got)
	}

	dir := t.TempDir()
	l := LoadFixLog(dir)
	now := time.Now()
	e := Event{Key: "k1", Ref: "acme/api#9", Workspace: "api", Kind: KindReviewRequested, At: now}
	l.StartFor(e, now.Add(-time.Hour))
	l.Finish("k1", nil, "", "", now.Add(-50*time.Minute))
	logs := map[string]string{"k1": "=== 2026-11-09T10:00:00Z review.requested acme/api#9\nacme/api#9: comments (1 summary), not approved.\n=== cost: $1\n"}
	l.Backfill(func(k string) string { return logs[k] }, nil, now)
	if rec := l.Records()[0]; rec.Said != "replied" {
		t.Fatalf("an old review run gets its outcome from the log: %+v", rec.Said)
	}
}
