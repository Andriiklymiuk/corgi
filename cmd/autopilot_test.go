package cmd

import (
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestAutopilotStatusMissingStateReportsUninitialized(t *testing.T) {
	dir := t.TempDir()
	st, err := loadAutopilotStatus(dir)
	if err != nil {
		t.Fatalf("status on empty dir should not error: %v", err)
	}
	// No file yet → a first run, distinct from an explicit stop, so the loop
	// starts rather than treating it as the kill switch. Never a crash.
	if st.Mode != utils.AutopilotUninitialized {
		t.Fatalf("empty status mode = %q, want uninitialized", st.Mode)
	}
}

func TestAutopilotPauseResumeStopTransitions(t *testing.T) {
	dir := t.TempDir()
	path := utils.AutopilotStatePath(dir)

	if _, err := utils.SetAutopilotMode(path, utils.AutopilotRunning); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if st, _ := utils.ReadAutopilotState(path); st.Mode != utils.AutopilotRunning {
		t.Fatalf("want running, got %q", st.Mode)
	}
	if _, err := utils.SetAutopilotMode(path, utils.AutopilotPaused); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if st, _ := utils.ReadAutopilotState(path); st.Mode != utils.AutopilotPaused {
		t.Fatalf("want paused, got %q", st.Mode)
	}
	if _, err := utils.SetAutopilotMode(path, utils.AutopilotStopped); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if filepath.Base(path) != ".autopilot.json" {
		t.Fatalf("unexpected path")
	}
}
