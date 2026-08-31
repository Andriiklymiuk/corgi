package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// A command that exits on an error path must still close the session log.
// logWriter holds a trailing line that has no newline yet, and only Close
// flushes it — so a bare os.Exit dropped whatever was written last.
func TestExitProcessFlushesSessionLog(t *testing.T) {
	dir := t.TempDir()
	origPath := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() {
		utils.CorgiComposePathDir = origPath
		utils.CloseSessionLog()
	})

	utils.StartSessionLog()
	logDir := filepath.Join(dir, "corgi_services", ".logs", utils.SessionLogDir)
	if _, err := os.Stat(logDir); err != nil {
		t.Skipf("session log not started: %v", err)
	}

	utils.Infof("partial line with no newline")

	code := -1
	orig := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = orig })

	exitProcess(1)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	entries, err := os.ReadDir(logDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no session log written: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "partial line with no newline") {
		t.Errorf("session log lost the trailing line, got %q", body)
	}
}
