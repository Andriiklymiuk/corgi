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

const doneShows = 24 * time.Hour

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

func (l *ArchiveLog) Known(key string, lastAt time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.Rows {
		if r.Key == key {
			return !lastAt.After(r.LastAt)
		}
	}
	return false
}

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
