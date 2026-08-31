// Package events keeps a small per-workspace timeline of supervisor lifecycle
// events. It never records session output, which can carry secrets.
package events

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	PID    int       `json:"pid,omitempty"`
	Cause  string    `json:"cause,omitempty"`
	Reason string    `json:"reason,omitempty"`
	URL    string    `json:"url,omitempty"`
}

const (
	maxFileBytes = 64 << 10
	maxKeep      = 200
)

type Log struct {
	dir string
	mu  sync.Mutex
}

func NewLog(dir string) *Log {
	return &Log{dir: filepath.Join(dir, "events")}
}

func (l *Log) Append(workspaceID string, e Event) {
	if l == nil || workspaceID == "" {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return
	}
	path := l.path(workspaceID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, werr := f.Write(append(line, '\n'))
	_ = f.Close()
	if werr != nil {
		return
	}
	l.trimLocked(path)
}

func (l *Log) trimLocked(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxFileBytes {
		return
	}
	lines := readLines(path)
	if len(lines) > maxKeep {
		lines = lines[len(lines)-maxKeep:]
	}
	_ = atomicfile.Write(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func (l *Log) Read(workspaceID string, limit int) []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	lines := readLines(l.path(workspaceID))
	l.mu.Unlock()

	out := make([]Event, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var e Event
		if json.Unmarshal([]byte(lines[i]), &e) != nil {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (l *Log) path(workspaceID string) string {
	return filepath.Join(l.dir, sanitizeID(workspaceID)+".jsonl")
}

func sanitizeID(id string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-", "..", "-").Replace(id)
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, string(line))
		}
	}
	return out
}
