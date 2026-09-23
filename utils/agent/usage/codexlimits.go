package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexWindow is codex's own rate-limit reading, as its newest session wrote
// it: the fullest window (five-hour or weekly) and when that one resets.
type CodexWindow struct {
	Percent  int
	ResetsAt time.Time
	Reached  bool
	At       time.Time
}

// Spent says the window is used up and has not reset yet.
func (w CodexWindow) Spent(now time.Time) bool {
	if !w.ResetsAt.IsZero() && !now.Before(w.ResetsAt) {
		return false
	}
	return w.Reached || w.Percent >= 98
}

func CodexHome() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// ReadCodexWindow reads the rate limits codex logged last, from the newest
// rollouts under <home>/sessions/YYYY/MM/DD. ok is false when there is none.
func ReadCodexWindow(home string) (CodexWindow, bool) {
	if home == "" {
		return CodexWindow{}, false
	}
	for _, path := range newestRollouts(filepath.Join(home, "sessions"), 3) {
		if w, ok := codexWindowOf(path); ok {
			return w, true
		}
	}
	return CodexWindow{}, false
}

func newestRollouts(root string, limit int) []string {
	var out []string
	levels := []string{root}
	for depth := 0; depth < 3; depth++ {
		var next []string
		for _, dir := range levels {
			next = append(next, sortedEntries(dir, true)...)
		}
		if len(next) > 4 {
			next = next[:4]
		}
		levels = next
	}
	for _, day := range levels {
		files := sortedEntries(day, false)
		sort.Slice(files, func(i, j int) bool { return modTime(files[i]).After(modTime(files[j])) })
		for _, f := range files {
			if strings.HasSuffix(f, ".jsonl") {
				out = append(out, f)
			}
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func sortedEntries(dir string, dirs bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() == dirs {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func codexWindowOf(path string) (CodexWindow, bool) {
	f, err := os.Open(path)
	if err != nil {
		return CodexWindow{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return CodexWindow{}, false
	}
	start := max(info.Size()-contextTail, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return CodexWindow{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, contextTail))
	if err != nil {
		return CodexWindow{}, false
	}
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		if w, ok := codexWindowFromLine(lines[i]); ok {
			return w, true
		}
	}
	return CodexWindow{}, false
}

type codexLimit struct {
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    int64   `json:"resets_at"`
}

func codexWindowFromLine(line []byte) (CodexWindow, bool) {
	if !bytes.Contains(line, []byte(`"rate_limits"`)) {
		return CodexWindow{}, false
	}
	var row struct {
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			Limits *struct {
				Primary   *codexLimit `json:"primary"`
				Secondary *codexLimit `json:"secondary"`
				Reached   *string     `json:"rate_limit_reached_type"`
			} `json:"rate_limits"`
		} `json:"payload"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &row) != nil || row.Payload.Limits == nil {
		return CodexWindow{}, false
	}
	lim := row.Payload.Limits
	w := CodexWindow{At: row.Timestamp, Reached: lim.Reached != nil && *lim.Reached != ""}
	found := false
	for _, l := range []*codexLimit{lim.Primary, lim.Secondary} {
		if l == nil {
			continue
		}
		found = true
		if p := int(l.UsedPercent); p >= w.Percent {
			w.Percent = p
			if l.ResetsAt > 0 {
				w.ResetsAt = time.Unix(l.ResetsAt, 0)
			}
		}
	}
	return w, found
}
