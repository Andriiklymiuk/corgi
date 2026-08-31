// Package command is the daemon's inbound mailbox: one JSON file per request,
// written by another corgi process (the MCP server or the CLI) and consumed by
// the running daemon. Files rather than a socket, to match the daemon's
// design — see daemon.go's statusPublishInterval comment.
package command

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ActionStart = "start"
	ActionStop  = "stop"
	// ActionAttention is a Claude Code hook reporting that a session wants a
	// person: a permission prompt, a question, or a finished turn.
	ActionAttention = "attention"
)

// TTL is how long a written command stays valid. A start that sat in the spool
// longer than this is deleted unexecuted: a clear failure now beats a session
// surprisingly appearing hours later.
const TTL = 60 * time.Second

// Command is one request to the daemon.
type Command struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	WorkspaceID string `json:"workspaceId"`
	Profile     string `json:"profile,omitempty"`
	// Name is the session name shown in claude.ai. Free text from the phone,
	// so the daemon sanitizes it before it reaches an argv.
	Name string `json:"name,omitempty"`
	// Detail carries a hook's own message, already trimmed by the sender.
	Detail      string    `json:"detail,omitempty"`
	Source      string    `json:"source,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
}

// Dir is the spool directory under the agent data dir.
func Dir(agentDir string) string { return filepath.Join(agentDir, "commands") }

// Write persists one command atomically and returns it with ID and
// RequestedAt filled.
func Write(agentDir string, c Command) (Command, error) {
	if c.Action != ActionStart && c.Action != ActionStop && c.Action != ActionAttention {
		return c, fmt.Errorf("unknown command action %q", c.Action)
	}
	if strings.TrimSpace(c.WorkspaceID) == "" {
		return c, fmt.Errorf("command needs a workspaceId")
	}
	if c.ID == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return c, err
		}
		c.ID = hex.EncodeToString(b[:])
	}
	if c.RequestedAt.IsZero() {
		c.RequestedAt = time.Now().UTC()
	}
	dir := Dir(agentDir)
	// 0700/0600: a spool entry starts an agent process, so only the owner may
	// write one.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return c, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	path := filepath.Join(dir, c.ID+".json")
	return c, atomicfile.Write(path, data, 0o600)
}

// Drain reads and removes every pending command, oldest first. Corrupt and
// stale files are deleted and skipped: the spool must never hold anything back
// for a second look.
func Drain(agentDir string, now time.Time, ttl time.Duration) ([]Command, error) {
	dir := Dir(agentDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Command
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, readErr := os.ReadFile(path)
		_ = os.Remove(path)
		if readErr != nil {
			continue
		}
		var c Command
		if json.Unmarshal(data, &c) != nil {
			continue
		}
		if c.RequestedAt.IsZero() || now.Sub(c.RequestedAt) > ttl {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.Before(out[j].RequestedAt) })
	return out, nil
}
