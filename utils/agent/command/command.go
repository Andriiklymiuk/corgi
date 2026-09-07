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

	"andriiklymiuk/corgi/utils/agent/sessions"
)

const (
	ActionStart = "start"
	ActionStop  = "stop"
	// ActionAttention is a Claude Code hook reporting that a session wants a
	// person: a permission prompt, a question, or a finished turn.
	ActionAttention = "attention"
	// ActionSession is one Claude Code hook event for the session registry,
	// written by `corgi agent hook emit`.
	ActionSession = "session"
	// ActionFocus, ActionPin and ActionPage are key presses: bring a session
	// to the front, reserve a key, turn the overflow page. ActionRescan asks
	// for a fresh look at the process table.
	ActionFocus  = "focus"
	ActionPin    = "pin"
	ActionPage   = "page"
	ActionRescan = "rescan"
	// ActionResize changes the number of keys on the board in place.
	ActionResize = "resize"
)

// needsWorkspace lists the actions addressed to a workspace; the rest are
// addressed to a session or to the board.
var needsWorkspace = map[string]bool{ActionStart: true, ActionStop: true, ActionAttention: true}

var known = map[string]bool{
	ActionStart: true, ActionStop: true, ActionAttention: true, ActionSession: true,
	ActionFocus: true, ActionPin: true, ActionPage: true, ActionRescan: true, ActionResize: true,
}

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

	// Event is the hook payload for ActionSession.
	Event *sessions.Event `json:"event,omitempty"`
	// SessionID names the target of ActionFocus: an id, an id prefix, a
	// label or a key number, resolved by the registry.
	SessionID string `json:"sessionId,omitempty"`
	// Index and Pinned are ActionPin's key and its new state.
	Index  int  `json:"index,omitempty"`
	Pinned bool `json:"pinned,omitempty"`
	// Direction is ActionPage's +1 (next) or -1 (previous).
	Direction int `json:"direction,omitempty"`
	// Size is ActionResize's new key count.
	Size int `json:"size,omitempty"`
}

// Dir is the spool directory under the agent data dir.
func Dir(agentDir string) string { return filepath.Join(agentDir, "commands") }

// validate rejects a command the daemon could not act on.
func (c Command) validate() error {
	if !known[c.Action] {
		return fmt.Errorf("unknown command action %q", c.Action)
	}
	if needsWorkspace[c.Action] && strings.TrimSpace(c.WorkspaceID) == "" {
		return fmt.Errorf("command needs a workspaceId")
	}
	switch c.Action {
	case ActionSession:
		if c.Event == nil || c.Event.SessionID == "" || c.Event.Name == "" {
			return fmt.Errorf("a session command needs an event with a session id and a name")
		}
	case ActionFocus:
		if strings.TrimSpace(c.SessionID) == "" {
			return fmt.Errorf("focus needs a session")
		}
	case ActionPage:
		if c.Direction == 0 {
			return fmt.Errorf("page needs a direction")
		}
	case ActionResize:
		if c.Size < 1 || c.Size > 64 {
			return fmt.Errorf("resize needs a size between 1 and 64")
		}
	}
	return nil
}

// Write persists one command atomically and returns it with ID and
// RequestedAt filled.
func Write(agentDir string, c Command) (Command, error) {
	if err := c.validate(); err != nil {
		return c, err
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
	data, err := json.Marshal(c)
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
