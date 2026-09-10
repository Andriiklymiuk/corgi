package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// StateLog is <agentDir>/watch/states.json: where a ticket sits now, once
// someone has moved it from here. The events log records the column a ticket
// was in when it arrived and is append-only, so without this a move you just
// made shows the old column until the tracker happens to send the ticket
// again — which, for an event already seen, is never.
type StateLog struct {
	mu     sync.Mutex
	path   string
	States map[string]TicketState `json:"states,omitempty"`
}

type TicketState struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

// stateKeep bounds the file; the inbox only ever shows the newest events.
const stateKeep = 300

func statesPath(agentDir string) string { return filepath.Join(agentDir, "watch", "states.json") }

// LoadStateLog reads the file; missing or broken is empty, never an error.
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

// Set records where a ticket went. The oldest entries are dropped once the
// file has grown past what the inbox can show.
func (l *StateLog) Set(key, status string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" || status == "" {
		return nil
	}
	l.States[key] = TicketState{Status: status, At: at}
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
