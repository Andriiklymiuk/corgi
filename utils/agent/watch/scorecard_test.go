package watch

import (
	"testing"
	"time"
)

func TestScorecardSortsRunsIntoOutcomes(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Minute)
	recs := []FixRecord{
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, GreenAt: now, PRs: []string{"x"}},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, Error: "exit status 143"},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, PRs: []string{"y"}},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, Said: "nothing"},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, Said: "replied"},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, Note: "in one run with a!1"},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour), FinishedAt: done, Error: "not started: blocked"},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-time.Hour)},
		{Workspace: "a", Kind: "ci.failed", StartedAt: now.Add(-30 * 24 * time.Hour), FinishedAt: done},
		{Workspace: "b", Kind: "pr.comment", StartedAt: now.Add(-time.Hour), FinishedAt: done},
	}
	rows := Scorecard(recs, "a", now.Add(-7*24*time.Hour))
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one for workspace a", rows)
	}
	r := rows[0]
	if r.Runs != 7 || r.Green != 1 || r.Pushed != 2 || r.Failed != 1 || r.Idle != 1 || r.Replied != 1 || r.Running != 1 {
		t.Errorf("row = %+v", r)
	}
}

func TestIdleKindsNamesOnlyKindsThatMostlyDoNothing(t *testing.T) {
	rows := []ScoreRow{
		{Workspace: "a", Kind: "pr.comment", Runs: 6, Idle: 5, Pushed: 1},
		{Workspace: "a", Kind: "ci.failed", Runs: 6, Idle: 2, Pushed: 4},
		{Workspace: "a", Kind: "review.requested", Runs: 9, Idle: 9},
		{Workspace: "a", Kind: "issue.new", Runs: 2, Idle: 2},
	}
	got := IdleKinds(rows)
	if len(got) != 1 || got[0].Kind != "pr.comment" {
		t.Errorf("IdleKinds = %+v, want pr.comment only", got)
	}
}

func TestRunOutcomeReadsTheLine(t *testing.T) {
	for out, want := range map[string]string{
		"**Outcome: pushed**\nFixed the flaky spec.": "pushed",
		"Outcome: nothing - it was a flake":          "nothing",
		"outcome: Replied":                           "replied",
		"I pushed it.":                               "",
	} {
		if got := RunOutcome(out); got != want {
			t.Errorf("RunOutcome(%q) = %q, want %q", out, got, want)
		}
	}
}
