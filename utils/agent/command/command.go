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
	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	ActionStart     = "start"
	ActionStop      = "stop"
	ActionAttention = "attention"
	ActionSession   = "session"
	ActionFocus     = "focus"
	ActionPin       = "pin"
	ActionPage      = "page"
	ActionRescan    = "rescan"
	ActionRefresh   = "refresh"
	ActionResize    = "resize"
	ActionNew       = "new"
	ActionDismiss   = "dismiss"
	ActionSend      = "send"
	ActionAnswer    = "answer"
	ActionNote      = "note"
	ActionCap       = "cap"
	ActionInterrupt = "interrupt"
	ActionContinue  = "continue"
	ActionRead      = "read"
	ActionWatch     = "watch"
	ActionPlan      = "plan"
)

var needsWorkspace = map[string]bool{ActionStart: true, ActionStop: true, ActionAttention: true}

var known = map[string]bool{
	ActionStart: true, ActionStop: true, ActionAttention: true, ActionSession: true,
	ActionFocus: true, ActionPin: true, ActionPage: true, ActionRescan: true, ActionRefresh: true, ActionResize: true, ActionNew: true,
	ActionDismiss:   true,
	ActionWatch:     true,
	ActionSend:      true,
	ActionAnswer:    true,
	ActionNote:      true,
	ActionCap:       true,
	ActionInterrupt: true,
	ActionRead:      true,
}

const TTL = 60 * time.Second

type Command struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"`
	WorkspaceID string    `json:"workspaceId"`
	Profile     string    `json:"profile,omitempty"`
	Name        string    `json:"name,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	Source      string    `json:"source,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`

	Event      *sessions.Event `json:"event,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Index      int             `json:"index,omitempty"`
	Pinned     bool            `json:"pinned,omitempty"`
	Direction  int             `json:"direction,omitempty"`
	Size       int             `json:"size,omitempty"`
	WindowID   string          `json:"windowId,omitempty"`
	Text       string          `json:"text,omitempty"`
	Enter      bool            `json:"enter,omitempty"`
	Note       string          `json:"note,omitempty"`
	Answer     string          `json:"answer,omitempty"`
	Tokens     int64           `json:"tokens,omitempty"`
	WatchEvent *watch.Event    `json:"watchEvent,omitempty"`
	Command    string          `json:"command,omitempty"`
}

func Dir(agentDir string) string { return filepath.Join(agentDir, "commands") }

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
	case ActionFocus, ActionInterrupt, ActionRead:
		if strings.TrimSpace(c.SessionID) == "" {
			return fmt.Errorf("%s needs a session", c.Action)
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
