package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

type StateLog struct {
	mu     sync.Mutex
	path   string
	States map[string]TicketState `json:"states,omitempty"`
}

type TicketState struct {
	Status string    `json:"status"`
	Was    string    `json:"was,omitempty"`
	At     time.Time `json:"at"`
}

const stateKeep = 300

func statesPath(agentDir string) string { return filepath.Join(agentDir, "watch", "states.json") }

func LoadStateLog(agentDir string) *StateLog {
	l := &StateLog{path: statesPath(agentDir), States: map[string]TicketState{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.States == nil {
		l.States = map[string]TicketState{}
	}
	return l
}

func (l *StateLog) Get(key string) (TicketState, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.States[key]
	return s, ok
}

func (l *StateLog) Set(key, status string, at time.Time) error {
	return l.SetFrom(key, status, "", at)
}

func (l *StateLog) SetFrom(key, status, was string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" || status == "" {
		return nil
	}
	if prev, ok := l.States[key]; ok && prev.Was != "" {
		was = prev.Was
	}
	l.States[key] = TicketState{Status: status, Was: was, At: at}
	for len(l.States) > stateKeep {
		oldestKey, oldest := "", time.Time{}
		for k, v := range l.States {
			if oldest.IsZero() || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(l.States, oldestKey)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

type RefStater interface {
	RefState(ctx context.Context, ref string) string
}

// Answerer says whether feedback on a pull request was answered after a moment:
// a reply by me and a push, both after it. That is what a fix by hand or an
// earlier run leaves behind, and a run on it would find nothing to do and still
// spend a session.
type Answerer interface {
	AnsweredSince(ctx context.Context, ref string, at time.Time) string
}
