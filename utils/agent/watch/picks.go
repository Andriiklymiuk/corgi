package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

type Pick struct {
	At time.Time `json:"at"`
	By string    `json:"by,omitempty"`
}

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
