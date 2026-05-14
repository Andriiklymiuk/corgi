package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLogWriter_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatalf("OpenLogWriter failed: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("hello log")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	w.Close()

	services, err := ListLoggedServices(dir)
	if err != nil {
		t.Fatalf("ListLoggedServices: %v", err)
	}
	if len(services) != 1 || services[0] != "api" {
		t.Errorf("expected [api], got %v", services)
	}
}

func TestOpenLogWriter_EmptyServiceName(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "")
	if err != nil {
		t.Fatal("expected no error for empty name")
	}
	if w != nil {
		t.Fatal("expected nil writer for empty name")
	}
}

func TestPruneLogs_KeepsLatestN(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, logsDirName, "api")
	os.MkdirAll(svcDir, 0o755)

	for i := 0; i < 15; i++ {
		name := filepath.Join(svcDir, strings.Repeat(string(rune('a'+i)), 1)+"_run.log")
		os.WriteFile(name, []byte("x"), 0o644)
	}

	PruneLogs(dir, "api", 10)

	entries, _ := os.ReadDir(svcDir)
	if len(entries) != 10 {
		t.Errorf("expected 10 log files after prune, got %d", len(entries))
	}
}

func TestPruneLogs_NoDir(t *testing.T) {
	// Should not panic when dir doesn't exist.
	PruneLogs(t.TempDir(), "nonexistent", 5)
}

func TestEnsureLogsGitignore_CreatesEntry(t *testing.T) {
	dir := t.TempDir()
	EnsureLogsGitignore(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("gitignore not created: %v", err)
	}
	if !strings.Contains(string(data), ".logs/") {
		t.Errorf(".logs/ not found in .gitignore: %s", data)
	}
}

func TestEnsureLogsGitignore_Idempotent(t *testing.T) {
	dir := t.TempDir()
	EnsureLogsGitignore(dir)
	EnsureLogsGitignore(dir)

	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	count := strings.Count(string(data), ".logs/")
	if count != 1 {
		t.Errorf("expected .logs/ exactly once, found %d times", count)
	}
}

func TestListServiceRuns_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, logsDirName, "api")
	os.MkdirAll(svcDir, 0o755)

	names := []string{"2024-01-01T10-00-00.log", "2024-01-03T10-00-00.log", "2024-01-02T10-00-00.log"}
	for _, n := range names {
		os.WriteFile(filepath.Join(svcDir, n), []byte("x"), 0o644)
	}

	runs, err := ListServiceRuns(dir, "api")
	if err != nil {
		t.Fatalf("ListServiceRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
	// Newest first: 2024-01-03 > 2024-01-02 > 2024-01-01
	if !strings.Contains(runs[0], "2024-01-03") {
		t.Errorf("expected newest first, got %v", runs)
	}
}

func TestListServiceRuns_SuffixDoesNotBreakOrder(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, logsDirName, "api")
	os.MkdirAll(svcDir, 0o755)

	names := []string{
		"2024-01-01T10-00-00.crashed.log",
		"2024-01-03T10-00-00.log",
		"2024-01-02T10-00-00.ok.log",
	}
	for _, n := range names {
		os.WriteFile(filepath.Join(svcDir, n), []byte("x"), 0o644)
	}

	runs, _ := ListServiceRuns(dir, "api")
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
	if !strings.Contains(runs[0], "2024-01-03") {
		t.Errorf("newest (2024-01-03) should sort first regardless of suffix, got %v", runs)
	}
	if !strings.Contains(runs[2], "2024-01-01") {
		t.Errorf("oldest (2024-01-01) should sort last regardless of suffix, got %v", runs)
	}
}

func TestRunSortKey_StripsStatusSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/tmp/.logs/api/2024-01-01T10-00-00.log", "2024-01-01T10-00-00"},
		{"/tmp/.logs/api/2024-01-01T10-00-00.crashed.log", "2024-01-01T10-00-00"},
		{"/tmp/.logs/api/2024-01-01T10-00-00.ok.log", "2024-01-01T10-00-00"},
	}
	for _, tc := range cases {
		if got := runSortKey(tc.in); got != tc.want {
			t.Errorf("runSortKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLogWriter_PrefixesEveryLine(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("first\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	runs, _ := ListServiceRuns(dir, "api")
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	data, _ := os.ReadFile(runs[0])
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), data)
	}
	for _, line := range lines {
		if len(line) < LogTimestampLen {
			t.Fatalf("line shorter than timestamp prefix: %q", line)
		}
		if line[LogTimestampLen-1] != ' ' {
			t.Errorf("expected space at index %d, got %q", LogTimestampLen-1, line)
		}
	}
	if !strings.HasSuffix(lines[0], " first") {
		t.Errorf("first line wrong: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], " second") {
		t.Errorf("second line wrong: %q", lines[1])
	}
}

func TestLogWriter_BuffersPartialLine(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("partial "))
	w.Write([]byte("more\n"))
	w.Close()

	runs, _ := ListServiceRuns(dir, "api")
	data, _ := os.ReadFile(runs[0])
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line (chunks merged), got %d: %q", len(lines), data)
	}
	if !strings.HasSuffix(lines[0], " partial more") {
		t.Errorf("expected merged 'partial more', got: %q", lines[0])
	}
}

func TestLogWriter_RenamesOnCrashedStatus(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	lw := w.(*logWriter)
	origPath := lw.Path()
	w.Write([]byte("oops\n"))
	lw.SetStatus(LogStatusCrashed)
	w.Close()

	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Errorf("original path should be gone, got: %v", err)
	}
	if !strings.HasSuffix(lw.Path(), ".crashed.log") {
		t.Errorf("expected .crashed.log suffix, got %s", lw.Path())
	}
	if _, err := os.Stat(lw.Path()); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
}

func TestLogWriter_RenamesOnOKStatus(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	lw := w.(*logWriter)
	lw.SetStatus(LogStatusOK)
	w.Close()

	if !strings.HasSuffix(lw.Path(), ".ok.log") {
		t.Errorf("expected .ok.log suffix, got %s", lw.Path())
	}
}

func TestLogWriter_NoRenameWhenStatusUnknown(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	lw := w.(*logWriter)
	origPath := lw.Path()
	w.Close()

	if lw.Path() != origPath {
		t.Errorf("expected no rename when status unknown, path changed to %s", lw.Path())
	}
}

func TestLogWriter_NormalizesWindowsNewlines(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello\r\nworld\r\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	runs, _ := ListServiceRuns(dir, "api")
	data, _ := os.ReadFile(runs[0])
	if strings.Contains(string(data), "\r") {
		t.Errorf("expected CR stripped, got: %q", data)
	}
	if strings.Count(string(data), "\n") != 2 {
		t.Errorf("expected exactly 2 newlines, got: %q", data)
	}
}

func TestLogWriter_FlushesPendingOnClose(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenLogWriter(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("no newline"))
	w.Close()

	runs, _ := ListServiceRuns(dir, "api")
	data, _ := os.ReadFile(runs[0])
	if !strings.HasSuffix(strings.TrimRight(string(data), "\n"), " no newline") {
		t.Errorf("expected flushed 'no newline', got: %q", data)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct{ in, out string }{
		{"simple", "simple"},
		{"with/slash", "with_slash"},
		{"with:colon", "with_colon"},
	}
	for _, tc := range cases {
		got := sanitizeName(tc.in)
		if got != tc.out {
			t.Errorf("sanitizeName(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}
