// Package transcript reads a Claude Code session's conversation as the
// phone shows it: what you said, what Claude said, each tool it used and
// what came back — from the JSONL Claude Code writes as it goes, tailed
// from where the last read stopped. Secrets that look like secrets are
// scrubbed before anything leaves the machine, results are cut short, and
// the machine decides which workspaces may be read at all (cmd/agent_stream).
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Entry is one thing that happened in the conversation.
type Entry struct {
	// ID is the row's uuid; the phone keys on it.
	ID string `json:"id"`
	// Kind: "user" (you typed), "assistant" (Claude said), "tool" (Claude
	// used a tool), "result" (what the tool returned).
	Kind string    `json:"kind"`
	At   time.Time `json:"at,omitzero"`
	Text string    `json:"text,omitempty"`
	// Tool and Subject for a tool call — "Edit", "auth/session.go".
	Tool    string `json:"tool,omitempty"`
	Subject string `json:"subject,omitempty"`
	// ToolID ties a result to its call.
	ToolID string `json:"toolId,omitempty"`
	// Truncated says the text was cut at MaxText.
	Truncated bool `json:"truncated,omitempty"`
}

// MaxText is where a message or a result is cut: a phone screen, not a log.
const MaxText = 2000

// MaxEntries is the most one read returns.
const MaxEntries = 200

const maxLine = 4 << 20

// Read returns the entries written after offset (a byte position from a
// previous read; 0 reads from the top) and the position to continue from.
// A file shorter than offset was replaced: it reads from the top again.
func Read(path string, offset int64, max int) ([]Entry, int64, error) {
	if max <= 0 || max > MaxEntries {
		max = MaxEntries
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && offset > st.Size() {
		offset = 0
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, offset, err
		}
	}
	r := bufio.NewReaderSize(f, 256<<10)
	var out []Entry
	for len(out) < max {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// A line still being written is read next time, whole.
			break
		}
		offset += int64(len(line))
		out = append(out, parse(line)...)
	}
	return out, offset, nil
}

// Last returns the newest max entries of the whole file, and the position
// after the last complete line — how a phone opens a conversation that has
// been going for hours without reading it all.
func Last(path string, max int) ([]Entry, int64, error) {
	if max <= 0 || max > MaxEntries {
		max = MaxEntries
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	ring := make([]Entry, 0, max*2)
	var offset int64
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += int64(len(line))
		ring = append(ring, parse(line)...)
		if len(ring) > max*2 {
			ring = append(ring[:0], ring[len(ring)-max:]...)
		}
	}
	if len(ring) > max {
		ring = ring[len(ring)-max:]
	}
	return ring, offset, nil
}

// Exists says whether there is a transcript to read.
func Exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// Size is the file's length, for the long-poll to notice growth.
func Size(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return st.Size()
}

// ErrNoTranscript is a session with nothing written yet.
var ErrNoTranscript = errors.New("no transcript yet")

type row struct {
	Type        string    `json:"type"`
	UUID        string    `json:"uuid"`
	Timestamp   time.Time `json:"timestamp"`
	IsMeta      bool      `json:"isMeta"`
	IsSidechain bool      `json:"isSidechain"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	ID      string          `json:"id"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	ToolUse string          `json:"tool_use_id"`
}

// parse turns one JSONL line into the entries a person reads. Thinking,
// images, system rows, hook chatter and sub-agent side chains stay out.
func parse(line []byte) []Entry {
	if len(line) > maxLine {
		return nil
	}
	var r row
	if json.Unmarshal(line, &r) != nil {
		return nil
	}
	if (r.Type != "user" && r.Type != "assistant") || r.IsMeta || r.IsSidechain || len(r.Message.Content) == 0 {
		return nil
	}
	var out []Entry
	// Content is a string (an old-style user prompt) or a list of blocks.
	var text string
	if json.Unmarshal(r.Message.Content, &text) == nil {
		if e, ok := textEntry(r, "user", text); ok {
			out = append(out, e)
		}
		return out
	}
	var blocks []block
	if json.Unmarshal(r.Message.Content, &blocks) != nil {
		return nil
	}
	for i, b := range blocks {
		id := r.UUID
		if i > 0 {
			id = r.UUID + "/" + itoa(i)
		}
		switch b.Type {
		case "text":
			kind := "assistant"
			if r.Type == "user" {
				kind = "user"
			}
			if e, ok := textEntry(r, kind, b.Text); ok {
				e.ID = id
				out = append(out, e)
			}
		case "tool_use":
			out = append(out, Entry{ID: id, Kind: "tool", At: r.Timestamp, Tool: b.Name, Subject: Scrub(subjectOf(b.Name, b.Input)), ToolID: b.ID})
		case "tool_result":
			t, truncated := clip(Scrub(resultText(b.Content)))
			out = append(out, Entry{ID: id, Kind: "result", At: r.Timestamp, Text: t, ToolID: b.ToolUse, Truncated: truncated})
		}
	}
	return out
}

func textEntry(r row, kind, text string) (Entry, bool) {
	text = strings.TrimSpace(text)
	// Claude Code's own reminders and command caveats are typed as the
	// user but are not the user.
	if text == "" || strings.HasPrefix(text, "<system-reminder>") || strings.HasPrefix(text, "<local-command") || strings.HasPrefix(text, "<command-") {
		return Entry{}, false
	}
	t, truncated := clip(Scrub(text))
	return Entry{ID: r.UUID, Kind: kind, At: r.Timestamp, Text: t, Truncated: truncated}, true
}

func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

func clip(s string) (string, bool) {
	if len(s) <= MaxText {
		return s, false
	}
	cut := s[:MaxText]
	if i := strings.LastIndexByte(cut, '\n'); i > MaxText/2 {
		cut = cut[:i]
	}
	return cut + "\n…", true
}

// subjectOf is the one thing a tool call is about: the file, the command,
// the pattern. Never the whole input.
func subjectOf(name string, input json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	str := func(k string) string {
		if v, ok := in[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	switch name {
	case "Bash":
		return firstLineOf(str("command"), 120)
	case "Edit", "Write", "Read", "NotebookEdit", "MultiEdit":
		return shortPath(str("file_path"))
	case "Grep", "Glob":
		if p := str("pattern"); p != "" {
			return firstLineOf(p, 80)
		}
	case "Agent", "Task":
		return firstLineOf(str("description"), 80)
	case "WebFetch", "WebSearch":
		if u := str("url"); u != "" {
			return u
		}
		return firstLineOf(str("query"), 80)
	}
	for _, k := range []string{"description", "path", "file_path", "query", "url"} {
		if v := str(k); v != "" {
			return firstLineOf(v, 80)
		}
	}
	return ""
}

func shortPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return p
}

func firstLineOf(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return itoa(i/10) + string(digits[i%10])
}

// Secrets that look like secrets, replaced before anything leaves the
// machine. It cannot catch everything — which workspaces may stream at
// all is the real control — but a token in a tool result should not ride
// to a phone because nobody looked.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(sk|rk|pk)[-_](?:live|test|ant|proj|or)?[-_]?[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[abpr]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`(?i)\b(bearer|token)\s+[A-Za-z0-9._~+/=-]{20,}`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|passwd|token|authorization)\s*[=:]\s*["']?[^\s"',]{6,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
}

// Scrub replaces what looks like a credential with a marker, keeping the
// name of the thing so the line still reads.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			// Keep "password=" / "Bearer " so the reader knows what was there.
			// A value already scrubbed, or one that is itself "Bearer …" (the
			// bearer pattern ran first), is left as it is.
			if strings.Contains(m, "•••") || strings.Contains(strings.ToLower(m), "bearer") && !strings.HasPrefix(strings.ToLower(m), "bearer") {
				return m
			}
			if i := strings.IndexAny(m, "=:"); i > 0 && i < 24 {
				return m[:i+1] + "•••"
			}
			if i := strings.IndexByte(m, ' '); i > 0 && i < 24 {
				return m[:i+1] + "•••"
			}
			return "•••"
		})
	}
	return s
}
