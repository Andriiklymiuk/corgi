package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

func TestTheBudgetLineDropsAWindowThatResetSince(t *testing.T) {
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	limits := &usage.Limits{
		FetchedAt: now.Add(-72 * time.Hour),
		FiveHour:  usage.Window{Percent: 96, ResetsAt: now.Add(-70 * time.Hour)},
		SevenDay:  usage.Window{Percent: 28, ResetsAt: now.Add(48 * time.Hour)},
	}
	st := sessions.State{Accounts: []sessions.Account{{ConfigDir: "/c", Limits: limits}}}
	got := budgetLine(st, "/c", now)
	if strings.Contains(got, "5h") || !strings.Contains(got, "week 28%") || strings.Contains(got, "nearly out") {
		t.Fatalf("a 5h window that reset three days ago is not this session's budget: %q", got)
	}
	if limits.FiveHour.Percent != 96 {
		t.Fatal("the board's own numbers stay as read")
	}
	limits.SevenDay.ResetsAt = now.Add(-time.Hour)
	if got := budgetLine(st, "/c", now); got != "" {
		t.Fatalf("both windows over: nothing to say: %q", got)
	}
}
