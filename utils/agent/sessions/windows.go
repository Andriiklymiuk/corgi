package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

func WindowsDir(agentDir string) string { return filepath.Join(agentDir, "windows") }

func RevealDir(agentDir string) string { return filepath.Join(agentDir, "reveal") }

func LoadWindows(agentDir string, alive func(pid int) bool) []Window {
	dir := WindowsDir(agentDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Window
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var w Window
		if json.Unmarshal(data, &w) != nil || w.ID == "" {
			_ = os.Remove(path)
			continue
		}
		if w.ExtHostPID > 0 && alive != nil && !alive(w.ExtHostPID) {
			_ = os.Remove(path)
			continue
		}
		if w.UpdatedAt.IsZero() {
			if info, err := e.Info(); err == nil {
				w.UpdatedAt = info.ModTime()
			}
		}
		out = append(out, w)
	}
	return out
}

type Reveal struct {
	WindowID    string    `json:"windowId"`
	SessionID   string    `json:"sessionId,omitempty"`
	ShellPID    int       `json:"shellPid,omitempty"`
	Panel       bool      `json:"panel,omitempty"`
	Title       string    `json:"title,omitempty"`
	New         bool      `json:"new,omitempty"`
	Folder      string    `json:"folder,omitempty"`
	Command     string    `json:"command,omitempty"`
	Text        string    `json:"text,omitempty"`
	Enter       bool      `json:"enter,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
}

func WriteReveal(agentDir string, req Reveal) error {
	if req.WindowID == "" {
		return nil
	}
	dir := RevealDir(agentDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, safeName(req.WindowID)+".json"), data, 0o600)
}

func safeName(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, id)
}
