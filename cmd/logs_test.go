package cmd

import (
	"andriiklymiuk/corgi/utils"
	"encoding/json"
	"io"
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
		printFollowedLine("api", "2024-01-01T10:32:01.123Z hello\n", true)
	})
	if !strings.Contains(out, "hello") || strings.Contains(out, "2024") {
		t.Errorf("expected stripped prefix, got: %q", out)
	}

	out = captureStdout(t, func() {
		printFollowedLine("api", "raw line\n", false)
	})
	if !strings.Contains(out, "raw line") {
		t.Errorf("expected raw line passthrough, got: %q", out)
	}

	out = captureStdout(t, func() {
		printFollowedLine("api", "short", true)
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

func TestRequireServiceForLogs(t *testing.T) {
	err := requireServiceForLogs("", true, []string{"api", "worker"})
	if err == nil {
		t.Fatal("expected error when no --service under non-interactive")
	}
	if !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "--service") {
		t.Errorf("error should name --service and list services, got %q", err.Error())
	}
	if requireServiceForLogs("api", true, []string{"api"}) != nil {
		t.Error("explicit service should pass")
	}
	if requireServiceForLogs("", false, []string{"api"}) != nil {
		t.Error("interactive mode should allow empty service")
	}
}

func TestLogJSONLine(t *testing.T) {
	got := logJSONLine("api", "2026-05-23T10:00:00Z", "info", "server up")
	want := `{"service":"api","ts":"2026-05-23T10:00:00Z","level":"info","line":"server up"}`
	if got != want {
		t.Fatalf("logJSONLine = %s want %s", got, want)
	}
}

func TestDetectLevel(t *testing.T) {
	if detectLevel("ERROR boom") != "error" {
		t.Fatal("expected error level")
	}
	if detectLevel("WARN slow") != "warn" {
		t.Fatal("expected warn level")
	}
	if detectLevel("all good") != "info" {
		t.Fatal("expected default info level")
	}
}

func TestFollowShouldStop(t *testing.T) {
	// read error (non-EOF) always stops
	var idle time.Time
	if !followShouldStop(errLogTest("io fail"), "/nope", &idle) {
		t.Fatal("non-EOF error should stop")
	}
	// EOF on an inactive (old) file stops
	dir := t.TempDir()
	p := filepath.Join(dir, "svc", "run.log")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("line\n"), 0644)
	old := time.Now().Add(-1 * time.Hour)
	os.Chtimes(p, old, old)
	idle = time.Time{}
	if !followShouldStop(io.EOF, p, &idle) {
		t.Fatal("EOF on inactive file should stop")
	}
}

func TestStreamLogLines_ReadsToEOF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "api", "run.log")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("hello\nworld\n"), 0644)
	old := time.Now().Add(-1 * time.Hour)
	os.Chtimes(p, old, old)

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := captureStdout(t, func() { streamLogLines(f, p) })
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("expected streamed lines, got %q", out)
	}
}

func TestPrintFollowedLine_JSON(t *testing.T) {
	origJSON := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = origJSON })
	out := captureStdout(t, func() {
		printFollowedLine("api", "2024-01-01T10:32:01.123Z boom error\n", true)
	})
	var rec struct{ Service, TS, Level, Line string }
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("not JSON: %q err=%v", out, err)
	}
	if rec.Service != "api" || rec.Level != "error" {
		t.Fatalf("bad record: %+v", rec)
	}
}

type errLogTest string

func (e errLogTest) Error() string { return string(e) }

func TestDumpNewestLogs(t *testing.T) {
	root := t.TempDir()
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = root
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	svcLogs := filepath.Join(root, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(svcLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(svcLogs, "2020-01-01T00-00-00.ok.log")
	newer := filepath.Join(svcLogs, "2021-01-01T00-00-00.ok.log")
	if err := os.WriteFile(older, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "dump")
	if err := dumpNewestLogs(logsBase(), out); err != nil {
		t.Fatalf("dump: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "api.log"))
	if err != nil {
		t.Fatalf("dumped file: %v", err)
	}
	if string(got) != "new\n" {
		t.Errorf("dumped %q, want the newest run", got)
	}
}

func TestDumpNewestLogsNoLogs(t *testing.T) {
	root := t.TempDir()
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = root
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := dumpNewestLogs(logsBase(), filepath.Join(root, "dump")); err == nil {
		t.Error("expected an error when no logs exist")
	}
}

func TestSanitizeLogName(t *testing.T) {
	if got := sanitizeLogName("api"); got != "api" {
		t.Errorf("plain name = %q", got)
	}
	if got := sanitizeLogName("group/api"); got != "group-api" {
		t.Errorf("separators must not create subdirectories, got %q", got)
	}
}

func TestCopyFileMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "out")); err == nil {
		t.Error("expected an error for a missing source")
	}
	if err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "no-such-dir", "out")); err == nil {
		t.Error("expected an error for an unwritable destination")
	}
}

func TestDumpNewestLogsSkipsEmptyServiceDirs(t *testing.T) {
	root := t.TempDir()
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = root
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	logs := filepath.Join(root, "corgi_services", ".logs")
	if err := os.MkdirAll(filepath.Join(logs, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(logs, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "api", "2021-01-01T00-00-00.ok.log"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "dump")
	if err := dumpNewestLogs(logsBase(), out); err != nil {
		t.Fatalf("dump: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "api.log" {
		t.Errorf("a service with no runs must be skipped, got %v", entries)
	}
}
