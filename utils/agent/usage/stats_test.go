package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const statsCache = `{
  "dailyActivity": [
    {"date": "2026-09-07", "messageCount": 1, "sessionCount": 1, "toolCallCount": 1},
    {"date": "2026-09-08", "messageCount": 42, "sessionCount": 3, "toolCallCount": 17}
  ],
  "dailyModelTokens": [
    {"date": "2026-09-08", "tokensByModel": {"claude-sonnet-4-5": 1000, "claude-opus-4-7": 250000}},
    {"date": "2026-09-09", "tokensByModel": {"claude-haiku-4-5": 5}}
  ]
}`

func writeStatsCache(t *testing.T, configDir string) {
	t.Helper()
	write(t, filepath.Join(configDir, "stats-cache.json"), []string{statsCache})
}

func TestReadDayStatsFindsTheDay(t *testing.T) {
	cfg := t.TempDir()
	writeStatsCache(t, cfg)
	day, ok := ReadDayStats(cfg, "2026-09-08")
	if !ok || day.Date != "2026-09-08" || day.Messages != 42 || day.Sessions != 3 || day.ToolCalls != 17 {
		t.Fatalf("day = %+v ok=%v", day, ok)
	}
	if len(day.Models) != 2 || day.Models[0].Model != "claude-opus-4-7" || day.Models[0].Tokens != 250000 || day.Models[1].Tokens != 1000 {
		t.Fatalf("models must be biggest first: %+v", day.Models)
	}
}

func TestReadDayStatsTokensAloneStillCount(t *testing.T) {
	cfg := t.TempDir()
	writeStatsCache(t, cfg)
	day, ok := ReadDayStats(cfg, "2026-09-09")
	if !ok || day.Messages != 0 || len(day.Models) != 1 || day.Models[0].Model != "claude-haiku-4-5" {
		t.Fatalf("day = %+v ok=%v", day, ok)
	}
	if day, ok := ReadDayStats(cfg, "2026-09-07"); !ok || day.Messages != 1 || len(day.Models) != 0 {
		t.Fatalf("activity without tokens: %+v ok=%v", day, ok)
	}
}

func TestReadDayStatsMissingDayCacheOrJSON(t *testing.T) {
	cfg := t.TempDir()
	writeStatsCache(t, cfg)
	if _, ok := ReadDayStats(cfg, "2020-01-01"); ok {
		t.Fatal("a day not in the cache is not found")
	}
	if _, ok := ReadDayStats(filepath.Join(cfg, "nope"), "2026-09-08"); ok {
		t.Fatal("no cache, no stats")
	}
	broken := t.TempDir()
	write(t, filepath.Join(broken, "stats-cache.json"), []string{"{"})
	if _, ok := ReadDayStats(broken, "2026-09-08"); ok {
		t.Fatal("a corrupt cache is no stats")
	}
}

func TestReadDayStatsDefaultsToHomeClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, ok := ReadDayStats("", "2026-09-08"); ok {
		t.Fatal("empty home has no cache")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeStatsCache(t, filepath.Join(home, ".claude"))
	if day, ok := ReadDayStats("", "2026-09-08"); !ok || day.Sessions != 3 {
		t.Fatalf("default account: %+v ok=%v", day, ok)
	}
}

func TestTodayIsTheLocalDate(t *testing.T) {
	now := time.Date(2026, 9, 8, 23, 30, 0, 0, time.FixedZone("east", 3*3600))
	if got := Today(now); got != now.Local().Format("2006-01-02") {
		t.Fatalf("got %q", got)
	}
	if got := Today(time.Date(2026, 1, 2, 12, 0, 0, 0, time.Local)); got != "2026-01-02" {
		t.Fatalf("got %q", got)
	}
}
