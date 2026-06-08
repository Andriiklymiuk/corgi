package utils

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAutopilotStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := AutopilotStatePath(dir)

	in := AutopilotState{
		Mode:        AutopilotRunning,
		Scope:       "agent",
		MaxBatch:    3,
		Iteration:   2,
		LastSummary: AutopilotIteration{Phase: "built", Built: 1, Skipped: 4},
	}
	if err := WriteAutopilotState(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadAutopilotState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Mode != AutopilotRunning || got.MaxBatch != 3 || got.LastSummary.Built != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("UpdatedAt not stamped on write")
	}
}

func TestAutopilotHeartbeatStampsTime(t *testing.T) {
	dir := t.TempDir()
	path := AutopilotStatePath(dir)
	_ = WriteAutopilotState(path, AutopilotState{Mode: AutopilotRunning})

	before := time.Now().Add(-time.Second)
	st, err := RecordAutopilotHeartbeat(path, AutopilotIteration{Phase: "idle"})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !st.LastHeartbeat.After(before) {
		t.Fatalf("heartbeat time not advanced: %v", st.LastHeartbeat)
	}
	if st.Iteration != 1 {
		t.Fatalf("iteration not incremented: %d", st.Iteration)
	}
}

func TestSetAutopilotModeMissingFileDefaults(t *testing.T) {
	dir := t.TempDir()
	path := AutopilotStatePath(dir)
	// No file yet: setting a mode should create one (resume on first run).
	st, err := SetAutopilotMode(path, AutopilotRunning)
	if err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if st.Mode != AutopilotRunning {
		t.Fatalf("mode = %q", st.Mode)
	}
	if filepath.Base(path) != ".autopilot.json" {
		t.Fatalf("unexpected state path %q", path)
	}
}
