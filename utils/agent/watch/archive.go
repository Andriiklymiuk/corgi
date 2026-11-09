package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// Archived is a card that left the board: finished, or quiet for so long
// that nothing was going to happen to it. It is the board's memory, not
// ignore: anything new about the ticket puts a card back.
type Archived struct {
	Key       string    `json:"key"`
	Ref       string    `json:"ref"`
	Workspace string    `json:"workspace,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Column    string    `json:"column"`
	Why       string    `json:"why"`
	Cost      *Cost     `json:"cost,omitempty"`
	LastAt    time.Time `json:"lastAt"`
	At        time.Time `json:"at"`
}

type ArchiveLog struct {
	mu   sync.Mutex
	path string
	Rows []Archived `json:"rows"`
}

const archiveKeep = 2000

func archivePath(agentDir string) string { return filepath.Join(agentDir, "watch", "archive.json") }

func LoadArchive(agentDir string) *ArchiveLog {
	l := &ArchiveLog{path: archivePath(agentDir)}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	return l
}

// Put takes a card off the board. The same key archived again replaces the
// older row, so a ticket that came back and left again is one memory.
func (l *ArchiveLog) Put(a Archived) error {
	if a.Key == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.Rows[:0]
	for _, r := range l.Rows {
		if r.Key != a.Key {
			kept = append(kept, r)
		}
	}
	l.Rows = append(kept, a)
	if len(l.Rows) > archiveKeep {
		l.Rows = l.Rows[len(l.Rows)-archiveKeep:]
	}
	return l.save()
}

// doneShows is how long a Done card stays in sight after it went into the
// history.
const doneShows = 24 * time.Hour

// Hidden says the board already let this card go, and nothing about it has
// happened since: a newer event on the same key brings it back. A card
// archived from Done still shows for a day.
func (l *ArchiveLog) Hidden(key string, lastAt, now time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.Rows {
		if r.Key != key {
			continue
		}
		if r.Column == "Done" && now.Sub(r.At) < doneShows {
			return false
		}
		return !lastAt.After(r.LastAt)
	}
	return false
}

// Since lists what left the board after that moment, newest first.
func (l *ArchiveLog) Since(since time.Time) []Archived {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Archived{}
	for _, r := range l.Rows {
		if r.At.After(since) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Forget lets a ticket back on the board by hand.
func (l *ArchiveLog) Forget(ref string) (Archived, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.Rows {
		if strings.EqualFold(r.Ref, ref) || r.Key == ref {
			l.Rows = append(l.Rows[:i], l.Rows[i+1:]...)
			_ = l.save()
			return r, true
		}
	}
	return Archived{}, false
}

func (l *ArchiveLog) save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}
