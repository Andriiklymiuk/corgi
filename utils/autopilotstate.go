package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type AutopilotMode string

const (
	AutopilotRunning AutopilotMode = "running"
	AutopilotPaused  AutopilotMode = "paused"
	AutopilotStopped AutopilotMode = "stopped"
)

// AutopilotIteration is the compact per-iteration summary the loop emits.
type AutopilotIteration struct {
	Phase    string `json:"phase"` // built | idle | awaiting_spec_signoff | error
	Built    int    `json:"built"`
	Skipped  int    `json:"skipped"`
	Awaiting int    `json:"awaiting"`
	Note     string `json:"note,omitempty"`
}

type AutopilotState struct {
	Mode          AutopilotMode      `json:"mode"`
	Scope         string             `json:"scope,omitempty"`
	MaxBatch      int                `json:"maxBatch,omitempty"`
	Iteration     int                `json:"iteration"`
	StartedAt     time.Time          `json:"startedAt,omitempty"`
	UpdatedAt     time.Time          `json:"updatedAt,omitempty"`
	LastHeartbeat time.Time          `json:"lastHeartbeat,omitempty"`
	LastSummary   AutopilotIteration `json:"lastSummary"`
}

func AutopilotStatePath(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".autopilot.json")
}

func WriteAutopilotState(path string, s AutopilotState) error {
	s.UpdatedAt = time.Now().UTC()
	if s.StartedAt.IsZero() {
		s.StartedAt = s.UpdatedAt
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadAutopilotState(path string) (AutopilotState, error) {
	var s AutopilotState
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

// readOrInit returns the existing state, or a fresh running default when absent.
func readOrInit(path string) AutopilotState {
	s, err := ReadAutopilotState(path)
	if err != nil {
		return AutopilotState{Mode: AutopilotRunning}
	}
	return s
}

// SetAutopilotMode flips mode, creating the file on first run (resume).
func SetAutopilotMode(path string, mode AutopilotMode) (AutopilotState, error) {
	s := readOrInit(path)
	s.Mode = mode
	return s, WriteAutopilotState(path, s)
}

// RecordAutopilotHeartbeat stamps the heartbeat, bumps the iteration counter,
// and stores the latest summary. Bounded, no goroutine.
func RecordAutopilotHeartbeat(path string, it AutopilotIteration) (AutopilotState, error) {
	s := readOrInit(path)
	s.Iteration++
	s.LastHeartbeat = time.Now().UTC()
	s.LastSummary = it
	return s, WriteAutopilotState(path, s)
}
