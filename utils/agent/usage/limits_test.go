package usage

import (
	"os"
	"path/filepath"
	"testing"
)

const cached = `{"cachedUsageUtilization":{"fetchedAtMs":1788863722380,"accountUuid":"x","utilization":{"five_hour":{"utilization":55,"resets_at":"2026-09-08T14:10:00.244000+00:00"},"seven_day":{"utilization":10.4,"resets_at":"2026-09-15T06:00:00.244018+00:00"},"seven_day_opus":null}}}`

func TestReadLimitsFromClaudeCodeCache(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, ".claude-work")
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, ".claude.json"), []byte(cached), 0o600); err != nil {
		t.Fatal(err)
	}
	l, ok := ReadLimits(custom)
	if !ok || l.FiveHour.Percent != 55 || l.SevenDay.Percent != 10 || l.FiveHour.ResetsAt.IsZero() || l.FetchedAt.IsZero() {
		t.Fatalf("limits: %+v %v", l, ok)
	}
	if _, ok := ReadLimits(filepath.Join(dir, "nothing")); ok {
		t.Fatal("no cache, no limits")
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(cached), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, ok := ReadLimits(filepath.Join(home, ".claude")); !ok || l.SevenDay.Percent != 10 {
		t.Fatalf("default account: %+v %v", l, ok)
	}
	if _, ok := parseLimits([]byte(`{"cachedUsageUtilization":{}}`)); ok {
		t.Fatal("an empty cache is no snapshot")
	}
}

func TestReadLimitsInTheNewerCacheShape(t *testing.T) {
	// Claude Code 2.1.2xx+ caches a fraction of one and unix seconds.
	newer := `{"cachedUsageUtilization":{"fetchedAtMs":1793184000000,"accountUuid":"x","utilization":{"five_hour":{"utilization":0.97,"resets_at":1793187600},"seven_day":{"utilization":0.24,"resets_at":1793254800},"seven_day_opus":null,"overage":{"utilization":null,"resets_at":null}}}}`
	l, ok := parseLimits([]byte(newer))
	if !ok {
		t.Fatal("the newer cache must read")
	}
	if l.FiveHour.Percent != 97 || l.SevenDay.Percent != 24 {
		t.Fatalf("a fraction of one is a percent: %+v", l)
	}
	if l.FiveHour.ResetsAt.Unix() != 1793187600 || l.SevenDay.ResetsAt.Unix() != 1793254800 {
		t.Fatalf("unix seconds are the reset: %+v", l)
	}
	ms := `{"cachedUsageUtilization":{"fetchedAtMs":1,"utilization":{"five_hour":{"utilization":100,"resets_at":1793187600000}}}}`
	if l, ok := parseLimits([]byte(ms)); !ok || l.FiveHour.Percent != 100 || l.FiveHour.ResetsAt.Unix() != 1793187600 {
		t.Fatalf("milliseconds and a whole percent read too: %+v %v", l, ok)
	}
}
