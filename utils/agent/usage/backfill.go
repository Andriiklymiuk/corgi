package usage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const backfillDays = 14

func (l *Ledger) NeedsBackfill() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.days) > 0 {
		return false
	}
	_, err := os.Stat(l.path)
	return os.IsNotExist(err)
}

func (l *Ledger) Backfill(ctx context.Context, configDirs []string, until time.Time) int {
	if l == nil {
		return 0
	}
	since := until.AddDate(0, 0, -backfillDays)
	read := 0
	seen := map[string]bool{}
	for _, dir := range configDirs {
		root := projectsRoot(dir)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		read += l.backfillRoot(ctx, root, since, until)
	}
	l.mu.Lock()
	l.dirty = true
	l.mu.Unlock()
	return read
}

func projectsRoot(dir string) string {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "projects")
}

func (l *Ledger) backfillRoot(ctx context.Context, root string, since, until time.Time) int {
	read := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().Before(since) {
			return nil
		}
		l.countTranscript(path, since, until)
		read++
		return nil
	})
	return read
}

var toolUseMark = []byte(`"type":"tool_use"`)

type backfillRow struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	IsMeta    bool   `json:"isMeta"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func (l *Ledger) countTranscript(path string, since, until time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), 64<<20)
	for sc.Scan() {
		var row backfillRow
		if json.Unmarshal(sc.Bytes(), &row) != nil || row.Timestamp == "" || row.SessionID == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err == nil && !at.Before(since) && at.Before(until) {
			l.noteRow(row, at)
		}
	}
}

func (l *Ledger) noteRow(row backfillRow, at time.Time) {
	switch row.Type {
	case "user":
		if row.IsMeta || len(row.Message.Content) == 0 || bytes.Contains(row.Message.Content, []byte(`"tool_result"`)) {
			l.Note("", row.SessionID, false, at)
			return
		}
		l.Note("UserPromptSubmit", row.SessionID, false, at)
	case "assistant":
		l.Note("", row.SessionID, false, at)
		for range bytes.Count(row.Message.Content, toolUseMark) {
			l.Note("PostToolUse", row.SessionID, false, at)
		}
	}
}
