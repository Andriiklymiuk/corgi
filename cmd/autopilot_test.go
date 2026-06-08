package cmd

import (
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestAutopilotStatusMissingStateReportsIdle(t *testing.T) {
	dir := t.TempDir()
	st, err := loadAutopilotStatus(dir)
	if err != nil {
		t.Fatalf("status on empty dir should not error: %v", err)
	}
	// No file yet → reported as stopped/uninitialized, never a crash.
	if st.Mode != utils.AutopilotStopped {
		t.Fatalf("empty status mode = %q, want stopped", st.Mode)
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
