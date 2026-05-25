package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type RunStateEntry struct {
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	PID             int       `json:"pid,omitempty"`
	PGID            int       `json:"pgid,omitempty"`
	Port            int       `json:"port,omitempty"`
	Container       string    `json:"container,omitempty"`
	Command         string    `json:"command,omitempty"`
	LogFile         string    `json:"logFile,omitempty"`
	Status          string    `json:"status"`
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

func RunStatePath(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".state.json")
}

// LockRunState takes an advisory lock so concurrent state mutations (restart,
// stop --service) don't clobber each other. Returns an unlock func. A lock held
// longer than the timeout is assumed stale and reclaimed.
func LockRunState(composeDir string) (func(), error) {
	lockPath := filepath.Join(composeDir, "corgi_services", ".state.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = os.Remove(lockPath) // stale; reclaim and retry
			deadline = time.Now().Add(5 * time.Second)
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}
}

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

func ReconcileRunState(
	s RunState,
	pidAlive func(pid int, command string) bool,
	containerState func(name string) string,
) RunState {
	now := time.Now().UTC()
	for i := range s.Services {
		e := &s.Services[i]
		// pid==0 → container-managed (docker-runner); can't probe by pid, leave as-is.
		if e.PID == 0 {
			continue
		}
		newStatus := "running"
		if !pidAlive(e.PID, e.Command) {
			newStatus = "crashed"
		}
		if newStatus != e.Status {
			e.Status = newStatus
			e.StatusChangedAt = now
		}
	}
	for i := range s.DBServices {
		e := &s.DBServices[i]
		newStatus := e.Status
		switch containerState(e.Container) {
		case "running":
			newStatus = "running"
		case "stopped":
			newStatus = "stopped"
		}
		if newStatus != e.Status {
			e.Status = newStatus
			e.StatusChangedAt = now
		}
	}
	return s
}

func ReadRunState(path string) (RunState, error) {
	var s RunState
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}
