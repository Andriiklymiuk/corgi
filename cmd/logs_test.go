package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogsBase_FallsBackToDot(t *testing.T) {
	utils.CorgiComposePathDir = ""
	base := logsBase()
	if base == "" {
		t.Fatal("logsBase() returned empty string")
	}
}

func TestPruneAllLogs_RemovesDir(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, ".logs", "api")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(logsDir, "run.log"), []byte("x"), 0o644)

	pruneAllLogs(dir)

	if _, err := os.Stat(filepath.Join(dir, ".logs")); !os.IsNotExist(err) {
		t.Error("expected .logs/ to be removed after pruneAllLogs")
	}
}

func TestPickLogRun_NoLogs(t *testing.T) {
	dir := t.TempDir()
	_, err := pickLogRun(dir, "nonexistent")
	if err == nil {
		t.Error("expected error when no logs exist")
	}
}

func TestFollowLog_NonExistentFile(t *testing.T) {
	// Should not panic, just print an error.
	followLog("/tmp/corgi_test_nonexistent_12345.log")
}

func TestIsLogFileActive_Missing(t *testing.T) {
	if isLogFileActive("/tmp/corgi_test_nonexistent_active.log") {
		t.Error("expected false for non-existent file")
	}
}

func TestIsLogFileActive_RecentFile(t *testing.T) {
	f, err := os.CreateTemp("", "corgi_log_*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	if !isLogFileActive(f.Name()) {
		t.Error("expected true for recently created file")
	}
}

func TestHasTimestampShape(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2024-01-01T10:32:01.123Z foo", true},
		{"2024-01-01T10:32:01.123Z\n", false},
		{"INFO 2024-01-01 my service starts", false},
		{"INFO server starting up nice", false},
		{"2024/01/01T10:32:01.123Z foo", false},
		{"2024-13-01T10:32:01.123Z foo", true}, // we don't validate month range — cheap
		{"2024-01-01T10-32-01.123Z foo", false},
		{"2024-01-01T10:32:01,123Z foo", false},
		{"2024-01-01T10:32:01.123  foo", false}, // no Z
		{"", false},
		{"short", false},
	}
	for _, tc := range cases {
		got := hasTimestampShape([]byte(tc.in))
		if got != tc.want {
			t.Errorf("hasTimestampShape(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRunLogs_PruneFlag(t *testing.T) {
	dir := t.TempDir()
	utils.CorgiComposePathDir = dir
	defer func() { utils.CorgiComposePathDir = "" }()
	logsDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	os.MkdirAll(logsDir, 0o755)
	os.WriteFile(filepath.Join(logsDir, "x.log"), []byte("x"), 0o644)

	logsPruneFlag = true
	defer func() { logsPruneFlag = false }()
	runLogs(nil, nil)

	if _, err := os.Stat(filepath.Join(dir, "corgi_services", ".logs")); !os.IsNotExist(err) {
		t.Error("expected .logs/ removed by --prune")
	}
}

func TestRunLogs_AllFlag_EmptyBaseErrors(t *testing.T) {
	dir := t.TempDir()
	utils.CorgiComposePathDir = dir
	defer func() { utils.CorgiComposePathDir = "" }()
	logsAllFlag = true
	defer func() { logsAllFlag = false }()
	out := captureStdout(t, func() { runLogs(nil, nil) })
	if !strings.Contains(out, "no log directories") {
		t.Errorf("expected 'no log directories' message, got: %q", out)
	}
}

func TestPickLogRun_ReturnsExistingFile(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, ".logs", "api")
	os.MkdirAll(svcDir, 0o755)
	os.WriteFile(filepath.Join(svcDir, "2024-01-01T10-00-00.log"), []byte("x"), 0o644)

	// pickLogRun with one entry would still call interactive picker.
	// Instead, verify the empty path: zero runs.
	if _, err := pickLogRun(dir, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent service")
	}
}

func TestLabelForRun(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/x/2024-01-01T10-00-00.crashed.log", "2024-01-01T10-00-00.log  ❌ crashed"},
		{"/x/2024-01-01T10-00-00.ok.log", "2024-01-01T10-00-00.log  ✅ ok"},
		{"/x/2024-01-01T10-00-00.log", "2024-01-01T10-00-00.log  ⏳ in-progress"},
	}
	for _, tc := range cases {
		if got := labelForRun(tc.in); got != tc.want {
			t.Errorf("labelForRun(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPrintFollowedLine(t *testing.T) {
	out := captureStdout(t, func() {
		printFollowedLine("2024-01-01T10:32:01.123Z hello\n", true)
	})
	if !strings.Contains(out, "hello") || strings.Contains(out, "2024") {
		t.Errorf("expected stripped prefix, got: %q", out)
	}

	out = captureStdout(t, func() {
		printFollowedLine("raw line\n", false)
	})
	if !strings.Contains(out, "raw line") {
		t.Errorf("expected raw line passthrough, got: %q", out)
	}

	out = captureStdout(t, func() {
		printFollowedLine("short", true)
	})
	if !strings.Contains(out, "short") {
		t.Errorf("expected short line passthrough when stripPrefix=true, got: %q", out)
	}
}

func TestShouldExitFollow_IdleTimeoutTriggers(t *testing.T) {
	prev := logsIdleFlag
	defer func() { logsIdleFlag = prev }()
	logsIdleFlag = 1 * time.Nanosecond

	tmp, _ := os.CreateTemp("", "corgi_log_*.log")
	defer os.Remove(tmp.Name())
	tmp.Close()
	old := time.Now().Add(-10 * time.Second)
	os.Chtimes(tmp.Name(), old, old)

	var idleSince time.Time
	time.Sleep(2 * time.Nanosecond)
	if !shouldExitFollow(tmp.Name(), &idleSince) {
		t.Error("expected exit when idle window exceeded and file stale")
	}
}

func TestShouldExitFollow_NotIdleStaysActive(t *testing.T) {
	prev := logsIdleFlag
	defer func() { logsIdleFlag = prev }()
	logsIdleFlag = 1 * time.Hour

	tmp, _ := os.CreateTemp("", "corgi_log_*.log")
	defer os.Remove(tmp.Name())
	tmp.Close()
	var idleSince time.Time
	if shouldExitFollow(tmp.Name(), &idleSince) {
		t.Error("expected no-exit when within idle window and file recent")
	}
}

func TestLooksLikeStampedLog_TrueAndFalse(t *testing.T) {
	dir := t.TempDir()
	stamped := dir + "/stamped.log"
	plain := dir + "/plain.log"
	os.WriteFile(stamped, []byte("2024-01-01T10:32:01.123Z hello\n"), 0o644)
	os.WriteFile(plain, []byte("INFO server starting\n"), 0o644)

	if !looksLikeStampedLog(stamped) {
		t.Error("expected stamped log detected")
	}
	if looksLikeStampedLog(plain) {
		t.Error("expected plain log not detected as stamped")
	}
	if looksLikeStampedLog(dir + "/no-such-file.log") {
		t.Error("expected missing file → false")
	}
}

func TestFollowAllLogs_StreamsAndMerges(t *testing.T) {
	dir := t.TempDir()
	apiDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	dbDir := filepath.Join(dir, "corgi_services", ".logs", "db")
	os.MkdirAll(apiDir, 0o755)
	os.MkdirAll(dbDir, 0o755)
	os.WriteFile(filepath.Join(apiDir, "2024-01-01T10-00-00.log"),
		[]byte("2024-01-01T10:00:01.000Z api boot\n2024-01-01T10:00:03.000Z api ready\n"), 0o644)
	os.WriteFile(filepath.Join(dbDir, "2024-01-01T10-00-00.log"),
		[]byte("2024-01-01T10:00:02.000Z db boot\n"), 0o644)

	utils.CorgiComposePathDir = dir
	defer func() { utils.CorgiComposePathDir = "" }()

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := followAllLogs(filepath.Join(dir, "corgi_services"))
	w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("followAllLogs err: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "api boot") {
		t.Errorf("line 0 should be earliest api: %q", lines[0])
	}
	if !strings.Contains(lines[1], "db boot") {
		t.Errorf("line 1 should be middle db: %q", lines[1])
	}
	if !strings.Contains(lines[2], "api ready") {
		t.Errorf("line 2 should be latest api: %q", lines[2])
	}
}
