package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// DoneMark says a person or a session dealt with a ticket or pull request
// at that moment: every inbox row about it from before then is handled.
// Rows that arrive afterwards show as usual - this is not ignore.
type DoneMark struct {
	At time.Time `json:"at"`
	By string    `json:"by,omitempty"`
}

type DoneLog struct {
	mu   sync.Mutex
	path string
	// Refs is by ticket or pull request ref, lowercased; Keys by one row's key.
	Refs map[string]DoneMark `json:"refs,omitempty"`
	Keys map[string]DoneMark `json:"keys,omitempty"`
}

const doneKeep = 500

func donePath(agentDir string) string { return filepath.Join(agentDir, "watch", "done.json") }

func LoadDoneLog(agentDir string) *DoneLog {
	l := &DoneLog{path: donePath(agentDir)}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Refs == nil {
		l.Refs = map[string]DoneMark{}
	}
	if l.Keys == nil {
		l.Keys = map[string]DoneMark{}
	}
	return l
}

// MarkRef: everything about ref up to at is handled.
func (l *DoneLog) MarkRef(ref string, at time.Time, by string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.Refs[ref]; ok && old.At.After(at) {
		return nil
	}
	l.Refs[ref] = DoneMark{At: at, By: by}
	return l.save()
}

// MarkKey: this one row is handled.
func (l *DoneLog) MarkKey(key string, at time.Time, by string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Keys[key] = DoneMark{At: at, By: by}
	return l.save()
}

// Handled says a mark covers the row: its key, or its ticket or any pull
// request it points at, marked at or after the row came in.
func (l *DoneLog) Handled(e Event) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.Keys[e.Key]; ok {
		return true
	}
	refs := append([]string{e.Ref}, PullRefsOf(e)...)
	for _, ref := range refs {
		if m, ok := l.Refs[strings.ToLower(ref)]; ok && ref != "" && !m.At.Before(e.At) {
			return true
		}
	}
	return false
}

func (l *DoneLog) save() error {
	prune(l.Refs)
	prune(l.Keys)
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func prune(m map[string]DoneMark) {
	for len(m) > doneKeep {
		oldestKey, oldest := "", time.Time{}
		for k, v := range m {
			if oldest.IsZero() || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(m, oldestKey)
	}
}
