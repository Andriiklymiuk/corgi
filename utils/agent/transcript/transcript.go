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

type Entry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at,omitzero"`
	Text      string    `json:"text,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	ToolID    string    `json:"toolId,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	// Picture names an image a tool returned (a screenshot); Picture(path,
	// id) hands the bytes over on demand so the entry itself stays small.
	Picture     string `json:"picture,omitempty"`
	PictureType string `json:"pictureType,omitempty"`
}

const MaxText = 2000

const MaxEntries = 200

const maxLine = 4 << 20

func Read(path string, offset int64, limit int) ([]Entry, int64, error) {
	if limit <= 0 || limit > MaxEntries {
		limit = MaxEntries
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
	for len(out) < limit {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += int64(len(line))
		out = append(out, parse(line)...)
	}
	return out, offset, nil
}

func Last(path string, limit int) ([]Entry, int64, error) {
	if limit <= 0 || limit > MaxEntries {
		limit = MaxEntries
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	ring := make([]Entry, 0, limit*2)
	var offset int64
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += int64(len(line))
		ring = append(ring, parse(line)...)
		if len(ring) > limit*2 {
			ring = append(ring[:0], ring[len(ring)-limit:]...)
		}
	}
	if len(ring) > limit {
		ring = ring[len(ring)-limit:]
	}
	return ring, offset, nil
}

func Exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func Size(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return st.Size()
}

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
			e := Entry{ID: id, Kind: "result", At: r.Timestamp, Text: t, ToolID: b.ToolUse, Truncated: truncated}
			if mime := resultPictureType(b.Content); mime != "" {
				e.Picture, e.PictureType = id, mime
			}
			out = append(out, e)
		case "image":
			if r.Type == "user" {
				out = append(out, Entry{ID: id, Kind: "user", At: r.Timestamp, Text: "🖼 image"})
			}
		}
	}
	return out
}

func textEntry(r row, kind, text string) (Entry, bool) {
	text = strings.TrimSpace(text)
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

func firstLineOf(s string, limit int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > limit {
		return s[:limit] + "…"
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

func Scrub(s string) string {
	if s == "" {
		return s
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
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
