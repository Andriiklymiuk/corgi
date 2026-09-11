package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// A pick is "Work on it" pressed: who asked for a session on a ticket and
// when. The session itself shows up a few seconds later, from its first
// hook event; until then the board would say nothing had happened. This
// file is that gap, and the record of who picked what.
type Pick struct {
	At time.Time `json:"at"`
	By string    `json:"by,omitempty"` // phone, page, cli, editor
}

// PickFresh is how long a pick explains a card with no session on it yet.
const PickFresh = 20 * time.Minute

type PickLog struct {
	mu    sync.Mutex
	path  string
	Picks map[string]Pick `json:"picks,omitempty"`
}

const pickKeep = 200

func picksPath(agentDir string) string { return filepath.Join(agentDir, "watch", "picks.json") }

func LoadPicks(agentDir string) *PickLog {
	l := &PickLog{path: picksPath(agentDir), Picks: map[string]Pick{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Picks == nil {
		l.Picks = map[string]Pick{}
	}
	return l
}

func (l *PickLog) Get(key string) (Pick, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.Picks[key]
	return p, ok
}

// Set records a pick; the oldest go once the file is full.
func (l *PickLog) Set(key, by string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" {
		return nil
	}
	l.Picks[key] = Pick{At: at, By: by}
	for len(l.Picks) > pickKeep {
		oldestKey, oldest := "", time.Time{}
		for k, v := range l.Picks {
			if oldest.IsZero() || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(l.Picks, oldestKey)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}
