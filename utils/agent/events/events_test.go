package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndReadNewestFirst(t *testing.T) {
	l := NewLog(t.TempDir())
	l.Append("acme", Event{Kind: "started", PID: 42})
	l.Append("acme", Event{Kind: "session", URL: "https://claude.ai/code/session_01A"})
	l.Append("acme", Event{Kind: "exited", Cause: "crash", Reason: "boom"})

	got := l.Read("acme", 0)
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3", len(got))
	}
	if got[0].Kind != "exited" || got[2].Kind != "started" {
		t.Errorf("order must be newest first, got %v then %v", got[0].Kind, got[2].Kind)
	}
	if got[0].At.IsZero() {
		t.Error("Append must stamp a time")
	}
}

func TestReadLimit(t *testing.T) {
	l := NewLog(t.TempDir())
	for i := 0; i < 5; i++ {
		l.Append("acme", Event{Kind: "started"})
	}
	if got := l.Read("acme", 2); len(got) != 2 {
		t.Errorf("limit 2 must cap the result, got %d", len(got))
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	l := NewLog(t.TempDir())
	if got := l.Read("nope", 0); len(got) != 0 {
		t.Errorf("a missing timeline is empty, got %v", got)
	}
}

func TestTrimKeepsNewestEntries(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	long := strings.Repeat("x", 400)
	for i := 0; i < 300; i++ {
		l.Append("acme", Event{Kind: "exited", Reason: long})
	}
	info, err := os.Stat(filepath.Join(dir, "events", "acme.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if info.Size() > int64(maxKeep*600) {
		t.Errorf("file did not trim: %d bytes", info.Size())
	}
	if got := l.Read("acme", 0); len(got) > maxKeep {
		t.Errorf("kept %d entries, cap is %d", len(got), maxKeep)
	}
}

func TestWorkspaceIDCannotEscapeTheDir(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.Append("../evil", Event{Kind: "started"})
	if _, err := os.Stat(filepath.Join(dir, "evil.jsonl")); err == nil {
		t.Error("a path-traversal id must not write outside events/")
	}
	if got := l.Read("../evil", 0); len(got) != 1 {
		t.Errorf("the sanitized id must still round-trip, got %d", len(got))
	}
}

func TestNilLogIsSafe(t *testing.T) {
	var l *Log
	l.Append("acme", Event{Kind: "started"})
	if got := l.Read("acme", 0); got != nil {
		t.Errorf("nil log reads empty, got %v", got)
	}
}

func TestExplicitTimestampKept(t *testing.T) {
	l := NewLog(t.TempDir())
	at := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	l.Append("acme", Event{Kind: "started", At: at})
	if got := l.Read("acme", 1); !got[0].At.Equal(at) {
		t.Errorf("timestamp rewritten: %v", got[0].At)
	}
}
