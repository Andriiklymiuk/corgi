package usage

import (
	"testing"
	"time"
)

func TestLedgerCountsSessionsOncePromptsAndToolCalls(t *testing.T) {
	dir := t.TempDir()
	l := OpenLedger(dir)
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local)
	l.Note("SessionStart", "s1", false, at)
	l.Note("UserPromptSubmit", "s1", false, at)
	l.Note("PostToolUse", "s1", false, at.Add(time.Minute))
	l.Note("PostToolUse", "s1", false, at.Add(2*time.Minute))
	l.Note("UserPromptSubmit", "s2", false, at.Add(time.Hour))
	l.Note("PostToolUse", "pid:42", false, at)
	l.Note("Stop", "", false, at)
	day := l.Day("2026-09-13")
	if day.Sessions != 2 || day.Messages != 2 || day.ToolCalls != 2 {
		t.Fatalf("got %+v", day)
	}
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := ReadLedgerDays(dir, []string{"2026-09-13", "2026-09-12"}); len(got) != 1 || got["2026-09-13"].Sessions != 2 {
		t.Fatalf("read back %+v", got)
	}
	// A later daemon picks up where this one stopped, ids included.
	again := OpenLedger(dir)
	again.Note("PostToolUse", "s1", false, at.Add(3*time.Minute))
	again.Note("PreToolUse", "s1", false, at.Add(3*time.Minute))
	again.Note("PreToolUse", "codex-1", true, at.Add(3*time.Minute))
	if day := again.Day("2026-09-13"); day.Sessions != 3 || day.ToolCalls != 4 {
		t.Fatalf("after reopen %+v", day)
	}
	if err := again.Flush(); err != nil {
		t.Fatal(err)
	}
	if l.Flush() != nil || (&Ledger{}).Day("x").Sessions != 0 {
		t.Fatal("a clean ledger flushes nothing; an empty one answers zero")
	}
}
