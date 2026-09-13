package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackfillCountsAFortnightOfTranscriptsOnce(t *testing.T) {
	cfg := t.TempDir()
	proj := filepath.Join(cfg, "projects", "-Users-me-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	opened := time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local)
	stamp := func(d time.Time) string { return d.UTC().Format(time.RFC3339Nano) }
	y := opened.AddDate(0, 0, -1)
	lines := "" +
		`{"type":"user","timestamp":"` + stamp(y) + `","sessionId":"a","message":{"content":"fix the login"}}` + "\n" +
		`{"type":"assistant","timestamp":"` + stamp(y.Add(time.Second)) + `","sessionId":"a","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","name":"Read"},{"type":"tool_use","name":"Edit"}]}}` + "\n" +
		`{"type":"user","timestamp":"` + stamp(y.Add(2*time.Second)) + `","sessionId":"a","message":{"content":[{"type":"tool_result","content":"done"}]}}` + "\n" +
		`{"type":"user","timestamp":"` + stamp(y.Add(3*time.Second)) + `","sessionId":"a","isMeta":true,"message":{"content":"<system>"}}` + "\n" +
		`{"type":"user","timestamp":"` + stamp(opened.Add(time.Minute)) + `","sessionId":"b","message":{"content":"after the ledger opened: the hooks count this"}}` + "\n" +
		`{"type":"queue-operation","timestamp":"` + stamp(y) + `","sessionId":"a"}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "a.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(proj, "old.jsonl")
	_ = os.WriteFile(old, []byte(`{"type":"user","timestamp":"`+stamp(y)+`","sessionId":"z","message":{"content":"stale"}}`+"\n"), 0o644)
	_ = os.Chtimes(old, opened.AddDate(0, 0, -40), opened.AddDate(0, 0, -40))

	agent := t.TempDir()
	l := OpenLedger(agent)
	if !l.NeedsBackfill() {
		t.Fatal("a new ledger needs the backfill")
	}
	if n := l.Backfill(context.Background(), []string{cfg, cfg}, opened); n != 1 {
		t.Fatalf("read %d transcripts, wanted the one touched this fortnight", n)
	}
	day := l.Day(Today(y))
	if day.Sessions != 1 || day.Messages != 1 || day.ToolCalls != 2 {
		t.Fatalf("yesterday %+v", day)
	}
	if l.Day(Today(opened)).Sessions != 0 {
		t.Fatal("lines after the opening are the hooks' to count")
	}
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	if OpenLedger(agent).NeedsBackfill() {
		t.Fatal("once is enough")
	}
}
