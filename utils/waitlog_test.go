package utils

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeServiceLog(t *testing.T, base, service, name, body string) string {
	t.Helper()
	dir := filepath.Join(base, ".logs", service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForLogLineIgnoresHistoryFromAnEarlierRun(t *testing.T) {
	base := t.TempDir()
	writeServiceLog(t, base, "api", "2026-01-01_000000.crashed.log", "Listening on :4000\n")

	_, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on", Timeout: 400 * time.Millisecond, Poll: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("a line from a previous run must not satisfy the wait")
	}
}

func TestWaitForLogLineMatchesALineWrittenWhileWaiting(t *testing.T) {
	base := t.TempDir()
	path := writeServiceLog(t, base, "api", "2026-01-01_000000.log", "booting\n")

	go func() {
		time.Sleep(150 * time.Millisecond)
		appendLine(t, path, "Listening on :4000\n")
	}()

	line, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on", Timeout: 4 * time.Second, Poll: 30 * time.Millisecond,
	})
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if line != "Listening on :4000" {
		t.Errorf("line = %q", line)
	}
}

func TestWaitForLogLineReadsHistoryWhenSinceIsSet(t *testing.T) {
	base := t.TempDir()
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	writeServiceLog(t, base, "api", "2026-01-01_000000.log", stamp+" Listening on :4000\n")

	line, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on",
		Since:   time.Now().Add(-time.Hour),
		Timeout: time.Second, Poll: 30 * time.Millisecond,
	})
	if err != nil || !matched {
		t.Fatalf("--since must let recent history count: matched=%v err=%v", matched, err)
	}
	if line != "Listening on :4000" {
		t.Errorf("line = %q", line)
	}
}

func TestWaitForLogLineSinceRejectsOlderLines(t *testing.T) {
	base := t.TempDir()
	old := time.Now().Add(-2 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	writeServiceLog(t, base, "api", "2026-01-01_000000.log", old+" Listening on :4000\n")

	_, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on",
		Since:   time.Now().Add(-time.Minute),
		Timeout: 400 * time.Millisecond, Poll: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("a line older than --since must not match")
	}
}

func TestWaitForLogLineHonoursTheDeadlineOnAChattyLog(t *testing.T) {
	base := t.TempDir()
	path := writeServiceLog(t, base, "api", "2026-01-01_000000.log", "")

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				appendLine(t, path, "noise\n")
				time.Sleep(time.Millisecond)
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })

	started := time.Now()
	_, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "never happens", Timeout: 500 * time.Millisecond, Poll: 20 * time.Millisecond,
	})
	if err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("a log that never goes quiet still has to time out, took %s", elapsed)
	}
}

func TestWaitForLogLineAdoptsANewerRun(t *testing.T) {
	base := t.TempDir()
	writeServiceLog(t, base, "api", "2026-01-01_000000.log", "old run\n")

	go func() {
		time.Sleep(150 * time.Millisecond)
		writeServiceLog(t, base, "api", "2026-01-02_000000.log", "Listening on :4000\n")
	}()

	line, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on", Timeout: 4 * time.Second, Poll: 30 * time.Millisecond,
	})
	if err != nil || !matched {
		t.Fatalf("a restart mid-wait must be followed: matched=%v err=%v", matched, err)
	}
	if line != "Listening on :4000" {
		t.Errorf("line = %q", line)
	}
}

func TestWaitForLogLineWaitsForTheFirstRun(t *testing.T) {
	base := t.TempDir()
	go func() {
		time.Sleep(120 * time.Millisecond)
		writeServiceLog(t, base, "api", "2026-01-01_000000.log", "Listening on :4000\n")
	}()

	_, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "Listening on", Timeout: 4 * time.Second, Poll: 30 * time.Millisecond,
	})
	if err != nil || !matched {
		t.Fatalf("a run that starts after the wait must match: matched=%v err=%v", matched, err)
	}
}

func TestWaitForLogLineRejectsBadPattern(t *testing.T) {
	if _, _, err := WaitForLogLine(t.TempDir(), LogWait{Service: "api", Pattern: "([", Timeout: time.Second}); err == nil {
		t.Error("an invalid regexp must be reported")
	}
}

func TestWaitForLogLineTimesOutWithNoLogAtAll(t *testing.T) {
	_, matched, err := WaitForLogLine(t.TempDir(), LogWait{
		Service: "api", Pattern: "anything", Timeout: 300 * time.Millisecond, Poll: 50 * time.Millisecond,
	})
	if err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
}

func TestReadBoundedLineCapsAnEndlessLine(t *testing.T) {
	base := t.TempDir()
	huge := make([]byte, maxWaitLineBytes+4096)
	for i := range huge {
		huge[i] = 'x'
	}
	path := writeServiceLog(t, base, "api", "2026-01-01_000000.log", "")
	go func() {
		time.Sleep(80 * time.Millisecond)
		appendLine(t, path, string(huge))
	}()

	_, matched, err := WaitForLogLine(base, LogWait{
		Service: "api", Pattern: "never", Timeout: 600 * time.Millisecond, Poll: 30 * time.Millisecond,
	})
	if err != nil || matched {
		t.Fatalf("a newline-less flood must not hang or match: matched=%v err=%v", matched, err)
	}
}
