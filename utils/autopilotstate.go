package utils

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type AutopilotMode string

const (
	AutopilotRunning       AutopilotMode = "running"
	AutopilotPaused        AutopilotMode = "paused"
	AutopilotStopped       AutopilotMode = "stopped"
	AutopilotUninitialized AutopilotMode = "uninitialized"
)

type AutopilotIteration struct {
	Phase    string `json:"phase"`
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
	return filepath.Join(CorgiServicesIn(composeDir), ".autopilot.json")
}

func WriteAutopilotState(path string, s AutopilotState) error {
	s.UpdatedAt = time.Now().UTC()
	if s.StartedAt.IsZero() {
		s.StartedAt = s.UpdatedAt
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(dir, filepath.Base(path))
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o644)
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

func readOrInit(path string) AutopilotState {
	s, err := ReadAutopilotState(path)
	if err != nil {
		return AutopilotState{Mode: AutopilotRunning}
	}
	return s
}

func SetAutopilotMode(path string, mode AutopilotMode) (AutopilotState, error) {
	s := readOrInit(path)
	s.Mode = mode
	return s, WriteAutopilotState(path, s)
}

func RecordAutopilotHeartbeat(path string, it AutopilotIteration) (AutopilotState, error) {
	s := readOrInit(path)
	s.Iteration++
	s.LastHeartbeat = time.Now().UTC()
	s.LastSummary = it
	return s, WriteAutopilotState(path, s)
}
