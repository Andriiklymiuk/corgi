package utils

import (
	"os"
	"path/filepath"
	"strings"
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

// Writing the state must keep it out of commits: the per-developer file is
// gitignored via corgi_services/.gitignore (the skill/docs promise "gitignored").
func TestWriteAutopilotStateGitignoresItself(t *testing.T) {
	dir := t.TempDir()
	path := AutopilotStatePath(dir) // <dir>/corgi_services/.autopilot.json
	if err := WriteAutopilotState(path, AutopilotState{Mode: AutopilotRunning}); err != nil {
		t.Fatalf("write: %v", err)
	}
	gi := filepath.Join(filepath.Dir(path), ".gitignore")
	body, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("expected a corgi_services/.gitignore, got: %v", err)
	}
	if !strings.Contains(string(body), ".autopilot.json") {
		t.Fatalf(".autopilot.json not ignored; .gitignore = %q", body)
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
