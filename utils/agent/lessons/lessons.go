// Package lessons keeps what a workspace learned the hard way: the review
// that said line 40 was wrong, the check that stayed red, the bot that
// fell over — one line each, oldest first, in a markdown file a person
// can read and edit. A new session in the workspace is pointed at it by
// the context hook, so the same thing is not learned twice.
package lessons

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Lesson is one line: when, where it came from, what it said.
type Lesson struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"` // "review acme/api#7 (dan)", "done-when", "bot reviewer", "you"
	Text   string    `json:"text"`
}

// Path is the workspace's file under the agent dir — never inside the
// repository, so nothing shows up in git status.
func Path(agentDir, workspace string) string {
	return filepath.Join(agentDir, "lessons", safe(workspace)+".md")
}

// Add appends one lesson. The text is one line; anything after the first
// newline is dropped, and a line already there is not written twice.
func Add(agentDir, workspace string, l Lesson) error {
	text := firstLine(l.Text)
	if text == "" {
		return fmt.Errorf("a lesson is a line of text")
	}
	if l.At.IsZero() {
		l.At = time.Now()
	}
	for _, have := range List(agentDir, workspace) {
		if have.Text == text {
			return nil
		}
	}
	path := Path(agentDir, workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		fmt.Fprintf(f, "# Lessons · %s\n\nWhat this workspace learned the hard way. One line each; edit freely.\n\n", workspace)
	}
	_, err = fmt.Fprintf(f, "- %s · %s: %s\n", l.At.Format("2006-01-02"), firstLine(l.Source), text)
	return err
}

// List reads the file back, oldest first. A line that is not a lesson —
// the heading, a note someone typed — is skipped.
func List(agentDir, workspace string) []Lesson {
	f, err := os.Open(Path(agentDir, workspace))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Lesson
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		out = append(out, parse(strings.TrimPrefix(line, "- ")))
	}
	return out
}

// parse reads "2026-09-13 · review acme/api#7 (dan): line 40 is wrong";
// a line without the date or the source is kept as text alone.
func parse(line string) Lesson {
	l := Lesson{Text: line}
	date, rest, ok := strings.Cut(line, " · ")
	if !ok {
		return l
	}
	at, err := time.Parse("2006-01-02", date)
	if err != nil {
		return l
	}
	l.At = at
	source, text, ok := strings.Cut(rest, ": ")
	if !ok {
		l.Text = rest
		return l
	}
	l.Source, l.Text = source, text
	return l
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "workspace"
	}
	return b.String()
}
