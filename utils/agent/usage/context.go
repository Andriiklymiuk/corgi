package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// Context is how full a session's context window is: what Claude Code's own
// status line shows, read from the transcript instead. Every assistant
// message carries the usage of the request that produced it, and the input
// side of the newest one (fresh input plus everything read from or written
// to the cache) is the context the next turn starts from.
type Context struct {
	Tokens  int64     `json:"tokens"`
	Window  int64     `json:"window"`
	Percent int       `json:"percent"`
	Model   string    `json:"model,omitempty"`
	At      time.Time `json:"at"`
}

// contextTail is how much of the transcript's end is read. A turn is one
// line and the last assistant line is what matters; 256 KiB spans even a
// large tool result sitting after it.
const contextTail = 256 << 10

// DefaultWindow is the context window of every current Claude model except
// the long-context variants, which Claude Code names with a "[1m]" suffix.
const DefaultWindow = 200_000

// WindowFor is the context window for a model name as the transcript spells
// it. A name nobody recognises gets the default: a wrong percentage is worse
// than a slightly conservative one.
func WindowFor(model string) int64 {
	m := strings.ToLower(model)
	if strings.Contains(m, "[1m]") || strings.Contains(m, "-1m") {
		return 1_000_000
	}
	return DefaultWindow
}

// ContextOf reads the newest assistant usage out of a transcript. ok is
// false when the file is missing, unreadable, or holds no assistant turn
// yet.
func ContextOf(transcriptPath string) (Context, bool) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return Context{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Context{}, false
	}
	start := info.Size() - contextTail
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return Context{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, contextTail))
	if err != nil {
		return Context{}, false
	}
	return contextFromTail(data, start > 0)
}

// contextFromTail scans lines from the end. When the read started mid-file
// the first line is a fragment and is dropped.
func contextFromTail(data []byte, truncated bool) (Context, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	first := 0
	if truncated {
		first = 1
	}
	for i := len(lines) - 1; i >= first; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var row struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Model string    `json:"model"`
				Usage *rawUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "assistant" || row.Message.Usage == nil {
			continue
		}
		u := row.Message.Usage
		tokens := u.Input + u.CacheRead + u.CacheWrite
		if tokens <= 0 {
			continue
		}
		c := Context{Tokens: tokens, Model: row.Message.Model, At: row.Timestamp, Window: WindowFor(row.Message.Model)}
		c.Percent = int(tokens * 100 / c.Window)
		if c.Percent > 100 {
			c.Percent = 100
		}
		return c, true
	}
	return Context{}, false
}
