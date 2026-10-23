package watch

import (
	"strings"
	"testing"
	"time"
)

func TestCarryKeepsWhenAPullRequestLastChanged(t *testing.T) {
	now := time.Now()
	was := PullStatus{State: "open", Checks: "passing", Review: "approved", At: now.Add(-time.Hour), Since: now.Add(-50 * time.Hour), Link: "https://github.com/a/b/pull/1", Workspace: "acme"}
	same := PullStatus{State: "open", Checks: "passing", Review: "approved", At: now}
	same.Carry(was, true, "", "", now)
	if !same.Since.Equal(was.Since) || same.Link != was.Link || same.Workspace != "acme" {
		t.Fatalf("nothing changed, so it has stood since then: %+v", same)
	}
	moved := PullStatus{State: "open", Checks: "failing", Review: "approved", At: now}
	moved.Carry(was, true, "", "", now)
	if !moved.Since.Equal(now) {
		t.Fatalf("a change starts the clock again: %+v", moved)
	}
}

func TestStalledPullsAreMineAndWaitingLongEnough(t *testing.T) {
	now := time.Now()
	pulls := map[string]PullStatus{
		"a/api#1": {State: "open", Checks: "passing", Review: "approved", Mine: true, Link: "https://github.com/a/api/pull/1", Since: now.Add(-50 * time.Hour)},
		"a/api#2": {State: "open", Checks: "passing", Review: "approved", Mine: true, Link: "https://github.com/a/api/pull/2", Since: now.Add(-2 * time.Hour)},
		"a/api#3": {State: "open", Checks: "passing", Review: "changes", Mine: true, Link: "https://github.com/a/api/pull/3", Since: now.Add(-72 * time.Hour)},
		"a/api#4": {State: "open", Checks: "failing", Review: "approved", Mine: false, Link: "https://github.com/a/api/pull/4", Since: now.Add(-72 * time.Hour)},
		"a/api#5": {State: "merged", Checks: "passing", Review: "approved", Mine: true, Link: "https://github.com/a/api/pull/5", Since: now.Add(-72 * time.Hour)},
	}
	got := StalledPulls(pulls, now)
	if len(got) != 2 {
		t.Fatalf("an approved green one waiting two days and one with changes asked for three: %+v", got)
	}
	if got[0].Ref != "a/api#3" || got[0].Action != "work" || !strings.Contains(got[0].Prompt, "pr threads") {
		t.Errorf("the oldest first, with what to tell a session: %+v", got[0])
	}
	if got[1].Ref != "a/api#1" || got[1].Action != "merge" || got[1].Why != "approved, checks ✓, not merged for 2 days" {
		t.Errorf("the ready one asks for a merge: %+v", got[1])
	}
}

func TestRunSpendCountsWhatTheWeekCostAndWhatItCameTo(t *testing.T) {
	now := time.Now()
	link := "https://github.com/a/api/pull/1"
	recs := []FixRecord{
		{Key: "1", Workspace: "a", Kind: string(KindIssueNew), StartedAt: now.Add(-time.Hour), FinishedAt: now, CostUSD: 3, PRs: []string{link}, Said: "pushed"},
		{Key: "2", Workspace: "a", Kind: string(KindCIFailed), StartedAt: now.Add(-2 * time.Hour), FinishedAt: now, CostUSD: 0.5, Said: "nothing"},
		{Key: "3", Workspace: "a", Kind: string(KindRoutine), StartedAt: now.Add(-3 * time.Hour), FinishedAt: now, CostUSD: 0.2, Said: "nothing"},
		{Key: "4", Workspace: "a", Kind: string(KindIssueNew), StartedAt: now.Add(-10 * 24 * time.Hour), FinishedAt: now, CostUSD: 9},
		{Key: "5", Workspace: "a", Kind: string(KindIssueNew), StartedAt: now.Add(-time.Hour), Error: "not started: blocked"},
	}
	s := RunSpendOf(recs, map[string]PullStatus{"a/api#1": {State: "merged"}}, now)
	if s.WeekRuns != 3 || s.WeekUSD != 3.7 || s.NothingRuns != 1 || s.NothingUSD != 0.5 || s.MergedPulls != 1 || s.PerMergedPull != 3.7 {
		t.Fatalf("three runs this week, a routine that reads is not idle work, one merged pull request: %+v", s)
	}
	if len(s.Days) != spendDays || s.Days[len(s.Days)-1].Runs != 3 {
		t.Fatalf("a fortnight of days, today last: %+v", s.Days)
	}
}

func TestSelfCheckSaysWhatCameBackAndWhatToDo(t *testing.T) {
	now := time.Now()
	recs := []FixRecord{
		{Key: "1", Workspace: "a", Kind: string(KindIssueNew), StartedAt: now.Add(-time.Hour), FinishedAt: now, Gaps: []Gap{GapNoDatabase}, Left: []LeftItem{{Text: "Run the SQL", Kind: LeftOther}}},
		{Key: "2", Workspace: "a", Kind: string(KindIssueNew), StartedAt: now.Add(-time.Hour), FinishedAt: now, Gaps: []Gap{GapNoDatabase}},
		{Key: "3", Workspace: "a", Kind: string(KindRoutine), StartedAt: now.Add(-time.Hour), FinishedAt: now, Error: EnvironmentPrefix + "offline"},
	}
	c := SelfCheckOf(recs, nil, now, 7)
	all := strings.Join(c.Lines, "\n")
	for _, want := range []string{"2 runs skipped a check: no database", "offline 1", "1 item left for you"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if c.Runs != 2 || !strings.HasPrefix(c.Headline(), "2 unattended runs in 7 days: 3 things") {
		t.Errorf("a run the machine stopped is not a run: %d %q", c.Runs, c.Headline())
	}
}
