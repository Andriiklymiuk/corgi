package lessons

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Lesson struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"`
	Text   string    `json:"text"`
}

func Path(agentDir, workspace string) string {
	return filepath.Join(agentDir, "lessons", safe(workspace)+".md")
}

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
