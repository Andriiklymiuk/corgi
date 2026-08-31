package utils

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeServiceLog(t *testing.T, base, service, body string) {
	t.Helper()
	dir := filepath.Join(base, ".logs", service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-01-01_000000.ok.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForLogLineMatches(t *testing.T) {
	base := t.TempDir()
	writeServiceLog(t, base, "api", "booting\nListening on :4000\n")

	line, matched, err := WaitForLogLine(base, "api", "Listening on", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || line != "Listening on :4000" {
		t.Fatalf("matched=%v line=%q", matched, line)
	}
}

func TestWaitForLogLineTimesOut(t *testing.T) {
	base := t.TempDir()
	writeServiceLog(t, base, "api", "booting\n")

	started := time.Now()
	_, matched, err := WaitForLogLine(base, "api", "never happens", 400*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("nothing should have matched")
	}
	if time.Since(started) < 300*time.Millisecond {
		t.Error("it returned before the timeout elapsed")
	}
}

func TestWaitForLogLineWaitsForTheFile(t *testing.T) {
	base := t.TempDir()
	_, matched, err := WaitForLogLine(base, "api", "anything", 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("no log file means no match")
	}
}

func TestWaitForLogLineRejectsBadPattern(t *testing.T) {
	if _, _, err := WaitForLogLine(t.TempDir(), "api", "([", time.Second); err == nil {
		t.Error("an invalid regexp must be reported")
	}
}

func TestWaitForLogLineStripsTheTimestampPrefix(t *testing.T) {
	base := t.TempDir()
	writeServiceLog(t, base, "api", "2026-01-01T00:00:00.000Z ready to serve\n")

	line, matched, err := WaitForLogLine(base, "api", "^ready", time.Second)
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if line != "ready to serve" {
		t.Errorf("line = %q, want the content without its timestamp", line)
	}
}
