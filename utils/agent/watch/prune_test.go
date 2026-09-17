package watch

import (
	"testing"
	"time"
)

func TestParseAgeReadsDaysAndDurations(t *testing.T) {
	if d, err := ParseAge("7d"); err != nil || d != 7*24*time.Hour {
		t.Fatalf("7d = %v, %v", d, err)
	}
	if d, err := ParseAge("48h"); err != nil || d != 48*time.Hour {
		t.Fatalf("48h = %v, %v", d, err)
	}
	if d, err := ParseAge(""); err != nil || d != 0 {
		t.Fatalf("empty = %v, %v", d, err)
	}
	for _, bad := range []string{"soon", "-1d", "xd"} {
		if _, err := ParseAge(bad); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
}

func TestPrunableFixesLeavesTheBusyAndTheYoung(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	records := []FixRecord{
		{Key: "a", Workspace: "ws", Branch: "corgi/ABC-1", StartedAt: old, FinishedAt: old},
		{Key: "b", Workspace: "ws", Branch: "corgi/ABC-2", StartedAt: old, FinishedAt: old},
		{Key: "b2", Workspace: "ws", Branch: "corgi/ABC-2", StartedAt: now.Add(-time.Hour)}, // still running
		{Key: "c", Workspace: "ws", Branch: "corgi/ABC-3", StartedAt: now.Add(-2 * time.Hour), FinishedAt: now.Add(-time.Hour)},
		{Key: "d", Workspace: "ws", Branch: "", StartedAt: old, FinishedAt: old}, // not isolated
		{Key: "a-again", Workspace: "ws", Branch: "corgi/ABC-1", StartedAt: old, FinishedAt: old},
	}
	got := PrunableFixes(records, 7*24*time.Hour, now)
	if len(got) != 1 || got[0].Branch != "corgi/ABC-1" {
		t.Fatalf("prunable = %+v, want only ABC-1 once", got)
	}
}
