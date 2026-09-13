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

// Backfill reads a fortnight of transcripts once, on the first daemon
// that has a ledger, so the card's bars are not empty for two weeks after
// an upgrade. Only lines older than the ledger's opening count: the hooks
// count everything after it. Every account's projects/ is walked; a
// transcript untouched for longer than the window is skipped unread.
const backfillDays = 14

// NeedsBackfill says whether the ledger was born just now: no file, and
// nothing counted yet.
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

// Backfill counts transcripts under each config dir's projects/ into the
// ledger, for lines stamped before `until`. It returns how many transcripts
// it read. ctx stops it between files.
func (l *Ledger) Backfill(ctx context.Context, configDirs []string, until time.Time) int {
	if l == nil {
		return 0
	}
	since := until.AddDate(0, 0, -backfillDays)
	read := 0
	seen := map[string]bool{}
	for _, dir := range configDirs {
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			dir = filepath.Join(home, ".claude")
		}
		root := filepath.Join(dir, "projects")
		if seen[root] {
			continue
		}
		seen[root] = true
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(since) {
				return nil
			}
			l.countTranscript(path, since, until)
			read++
			return nil
		})
	}
	l.mu.Lock()
	l.dirty = true
	l.mu.Unlock()
	return read
}

var toolUseMark = []byte(`"type":"tool_use"`)

// countTranscript folds one transcript's prompts, tool calls and session
// into the ledger by the local day of each line.
func (l *Ledger) countTranscript(path string, since, until time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			SessionID string `json:"sessionId"`
			IsMeta    bool   `json:"isMeta"`
			Message   struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.Timestamp == "" || row.SessionID == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil || at.Before(since) || !at.Before(until) {
			continue
		}
		switch row.Type {
		case "user":
			// A prompt is what a person typed: a string, or text blocks — a
			// tool_result array is the tool answering, not a person.
			if row.IsMeta || len(row.Message.Content) == 0 || bytes.Contains(row.Message.Content, []byte(`"tool_result"`)) {
				l.Note("", row.SessionID, false, at)
				continue
			}
			l.Note("UserPromptSubmit", row.SessionID, false, at)
		case "assistant":
			l.Note("", row.SessionID, false, at)
			for range bytes.Count(row.Message.Content, toolUseMark) {
				l.Note("PostToolUse", row.SessionID, false, at)
			}
		}
	}
}
