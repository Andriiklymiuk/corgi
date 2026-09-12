// Package bots keeps the named sessions a person comes back to: a bot is a
// workspace, a persona, a model and an account, with the thread it last ran
// — "Code Reviewer", "Shipper", "Chief". Opening one starts Claude Code in
// that workspace under that account with the persona appended to its system
// prompt, and resumes the last conversation when it can. Sessions are
// processes; bots are who you talk to.
package bots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bot is one named session identity.
type Bot struct {
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Workspace string `json:"workspace"`
	// Soul is appended to Claude Code's system prompt: who this bot is.
	Soul    string `json:"soul,omitempty"`
	Model   string `json:"model,omitempty"`
	Profile string `json:"profile,omitempty"`
	Isolate bool   `json:"isolate,omitempty"`
	// Color is a hue name the surfaces draw the avatar in.
	Color string `json:"color,omitempty"`
	// LastSession is the conversation to resume, and when it was last seen
	// — the daemon records it from the session's first event.
	LastSession string    `json:"lastSession,omitempty"`
	LastSeen    time.Time `json:"lastSeen,omitzero"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Display is the title, else the name.
func (b Bot) Display() string {
	if strings.TrimSpace(b.Title) != "" {
		return b.Title
	}
	return b.Name
}

// Store is every bot on this machine.
type Store struct {
	Version int   `json:"version"`
	Bots    []Bot `json:"bots"`
}

// Path is where bots live under the agent dir.
func Path(agentDir string) string { return filepath.Join(agentDir, "bots.json") }

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidName is a name safe on a command line and in a URL.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// Colors are the hues a surface may draw; the first is the default.
var Colors = []string{"indigo", "orange", "teal", "pink", "green", "amber", "blue", "red"}

var mu sync.Mutex

// Load reads the store; none is an empty store.
func Load(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return &s, nil
}

// Save writes the store atomically, 0600: a soul can say what a person
// would not put in a repository.
func Save(path string, s *Store) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sort.Slice(s.Bots, func(i, j int) bool { return s.Bots[i].Name < s.Bots[j].Name })
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the bot with that name.
func (s *Store) Find(name string) (Bot, bool) {
	for _, b := range s.Bots {
		if b.Name == name {
			return b, true
		}
	}
	return Bot{}, false
}

// Put adds or replaces a bot by name, keeping the thread it had.
func (s *Store) Put(b Bot) {
	for i := range s.Bots {
		if s.Bots[i].Name == b.Name {
			if b.LastSession == "" {
				b.LastSession, b.LastSeen = s.Bots[i].LastSession, s.Bots[i].LastSeen
			}
			b.CreatedAt = s.Bots[i].CreatedAt
			s.Bots[i] = b
			return
		}
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	s.Bots = append(s.Bots, b)
}

// Remove takes a bot out; reports whether it was there.
func (s *Store) Remove(name string) bool {
	for i := range s.Bots {
		if s.Bots[i].Name == name {
			s.Bots = append(s.Bots[:i], s.Bots[i+1:]...)
			return true
		}
	}
	return false
}

// RecordSession is what the daemon calls when a session opened for a bot
// sends its first event: that conversation is the one to resume next time.
// Load-modify-save under one lock, so two sessions starting at once do not
// lose each other.
func RecordSession(path, name, sessionID string, at time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := Load(path)
	if err != nil {
		return err
	}
	for i := range s.Bots {
		if s.Bots[i].Name == name {
			if s.Bots[i].LastSession == sessionID {
				return nil
			}
			s.Bots[i].LastSession, s.Bots[i].LastSeen = sessionID, at.UTC()
			return Save(path, s)
		}
	}
	return nil
}
