package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type RunStateEntry struct {
	Name            string    `json:"name"`
	Kind            string    `json:"kind"` // "service" | "db_service"
	PID             int       `json:"pid,omitempty"`
	PGID            int       `json:"pgid,omitempty"`
	Port            int       `json:"port,omitempty"`
	Container       string    `json:"container,omitempty"`
	Command         string    `json:"command,omitempty"`
	LogFile         string    `json:"logFile,omitempty"`
	Status          string    `json:"status"` // running | stopped | crashed | unknown
	StartedAt       time.Time `json:"startedAt,omitempty"`
	StatusChangedAt time.Time `json:"statusChangedAt,omitempty"`
	ExitCode        *int      `json:"exitCode,omitempty"`
}

type RunState struct {
	ComposePath string          `json:"composePath"`
	StartedAt   time.Time       `json:"startedAt,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt,omitempty"`
	Services    []RunStateEntry `json:"services"`
	DBServices  []RunStateEntry `json:"dbServices"`
}

// RunStatePath returns the state-file path for the project rooted at composeDir.
func RunStatePath(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".state.json")
}

// WriteRunState writes s atomically (temp + rename).
func WriteRunState(path string, s RunState) error {
	s.UpdatedAt = time.Now().UTC()
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

// ReadRunState reads and parses the state file.
func ReadRunState(path string) (RunState, error) {
	var s RunState
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}
