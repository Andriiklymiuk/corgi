package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// The corgi VS Code extension and the daemon meet on disk, like everything
// else in agent mode: each window writes windows/<id>.json and nudges the
// daemon; the daemon writes reveal/<id>.json and the window's extension picks
// it up. No port, no token — the agent directory is already owner-only.

// WindowsDir is where extension instances describe their windows.
func WindowsDir(agentDir string) string { return filepath.Join(agentDir, "windows") }

// RevealDir is where the daemon leaves tab-reveal requests.
func RevealDir(agentDir string) string { return filepath.Join(agentDir, "reveal") }

// LoadWindows reads every window record, dropping (and deleting) those whose
// extension host has exited: a crashed or force-quit window writes no
// goodbye. alive is injected; the daemon passes proc.Alive.
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

// Reveal is one request to an editor window: show this tab, or the panel.
type Reveal struct {
	WindowID  string `json:"windowId"`
	SessionID string `json:"sessionId,omitempty"`
	ShellPID  int    `json:"shellPid,omitempty"`
	Panel     bool   `json:"panel,omitempty"`
	// Title is the chat tab to pick when the window has several Claude
	// Code panels open; empty means whichever the editor considers current.
	Title string `json:"title,omitempty"`
	// New asks the window to open a fresh integrated terminal in Folder
	// and run claude in it — the "+" key.
	New    bool   `json:"new,omitempty"`
	Folder string `json:"folder,omitempty"`
	// Command is what the new terminal runs — `corgi agent claude`, which
	// applies the folder's workspace account — else the extension's default.
	Command string `json:"command,omitempty"`
	// Text asks the window to type into the session's terminal after
	// revealing it; Enter adds a carriage return. What `corgi agent send`
	// delivers when the session runs in an integrated terminal.
	Text        string    `json:"text,omitempty"`
	Enter       bool      `json:"enter,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
}

// WriteReveal leaves a request for one window. The extension deletes it once
// acted on; a request nobody reads is harmless and overwritten by the next.
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

// safeName keeps a window id usable as a file name. VS Code's session ids
// are already plain, but an id is data from another process.
func safeName(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, id)
}
