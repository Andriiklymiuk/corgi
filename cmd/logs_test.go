package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
