package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type DreamOptions struct {
	MaxAge  time.Duration
	Keep    int
	Similar float64
	DryRun  bool
}

var DefaultDream = DreamOptions{MaxAge: 180 * 24 * time.Hour, Keep: 60, Similar: 0.8}

type Dropped struct {
	Lesson Lesson `json:"lesson"`
	Why    string `json:"why"`
}

type DreamReport struct {
	Workspace string    `json:"workspace"`
	Before    int       `json:"before"`
	After     int       `json:"after"`
	Dropped   []Dropped `json:"dropped"`
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

func similarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	return float64(inter) / float64(union)
}

type line struct {
	raw    string
	lesson *Lesson
}

func readLines(path string) ([]line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []line
	for _, raw := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "- ") {
			l := parse(strings.TrimPrefix(t, "- "))
			out = append(out, line{raw: raw, lesson: &l})
			continue
		}
		out = append(out, line{raw: raw})
	}
	return out, nil
}

type indexed struct {
	i     int
	l     *Lesson
	words map[string]bool
}

func indexLessons(lines []line) []indexed {
	var all []indexed
	for i, ln := range lines {
		if ln.lesson != nil {
			all = append(all, indexed{i, ln.lesson, words(ln.lesson.Text)})
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].l.At.After(all[b].l.At) })
	return all
}

func markDuplicates(all []indexed, similar float64, drop map[int]string) {
	for a := range all {
		if _, gone := drop[all[a].i]; gone {
			continue
		}
		for b := a + 1; b < len(all); b++ {
			if _, gone := drop[all[b].i]; !gone && similarity(all[a].words, all[b].words) >= similar {
				drop[all[b].i] = "same as a newer lesson: " + all[a].l.Text
			}
		}
	}
}

func markRetired(all []indexed, opt DreamOptions, now time.Time, drop map[int]string) {
	kept := 0
	for _, x := range all {
		if _, gone := drop[x.i]; gone {
			continue
		}
		switch {
		case opt.MaxAge > 0 && !x.l.At.IsZero() && now.Sub(x.l.At) > opt.MaxAge:
			drop[x.i] = fmt.Sprintf("older than %d days", int(opt.MaxAge.Hours()/24))
		case opt.Keep > 0 && kept >= opt.Keep:
			drop[x.i] = fmt.Sprintf("past the newest %d", opt.Keep)
		default:
			kept++
		}
	}
}

func rewrite(path string, lines []line, drop map[int]string) error {
	var b strings.Builder
	for i, ln := range lines {
		if _, gone := drop[i]; !gone {
			b.WriteString(ln.raw)
			b.WriteByte('\n')
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Dream(agentDir, workspace string, now time.Time, opt DreamOptions) (DreamReport, error) {
	rep := DreamReport{Workspace: workspace}
	path := Path(agentDir, workspace)
	lines, err := readLines(path)
	if os.IsNotExist(err) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	all := indexLessons(lines)
	drop := map[int]string{}
	markDuplicates(all, opt.Similar, drop)
	markRetired(all, opt, now, drop)
	rep.Before, rep.After = len(all), len(all)-len(drop)
	for i, why := range drop {
		rep.Dropped = append(rep.Dropped, Dropped{Lesson: *lines[i].lesson, Why: why})
	}
	sort.Slice(rep.Dropped, func(a, b int) bool { return rep.Dropped[a].Lesson.At.Before(rep.Dropped[b].Lesson.At) })
	if opt.DryRun || len(drop) == 0 {
		return rep, nil
	}
	if err := archive(agentDir, workspace, now, rep.Dropped); err != nil {
		return rep, err
	}
	return rep, rewrite(path, lines, drop)
}

func ArchivePath(agentDir, workspace string) string {
	return filepath.Join(agentDir, "lessons", safe(workspace)+".dreamed.md")
}

func archive(agentDir, workspace string, now time.Time, dropped []Dropped) error {
	f, err := os.OpenFile(ArchivePath(agentDir, workspace), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		fmt.Fprintf(f, "# Lessons put away · %s\n\nWhat a dreaming pass took out, and why. Copy a line back into %s.md to keep it.\n\n", workspace, safe(workspace))
	}
	fmt.Fprintf(f, "## %s\n\n", now.Format("2006-01-02 15:04"))
	for _, d := range dropped {
		fmt.Fprintf(f, "- %s · %s: %s  ← %s\n", d.Lesson.At.Format("2006-01-02"), d.Lesson.Source, d.Lesson.Text, d.Why)
	}
	_, err = fmt.Fprintln(f)
	return err
}

func Workspaces(agentDir string) []string {
	entries, err := os.ReadDir(filepath.Join(agentDir, "lessons"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".dreamed.md") {
			continue
		}
		out = append(out, strings.TrimSuffix(n, ".md"))
	}
	return out
}

func markerPath(agentDir string) string { return filepath.Join(agentDir, "lessons", ".dreamed") }

func DreamDue(agentDir string, now time.Time, every time.Duration) bool {
	data, err := os.ReadFile(markerPath(agentDir))
	if err != nil {
		return len(Workspaces(agentDir)) > 0
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return err != nil || now.Sub(last) >= every
}

func DreamAll(agentDir string, now time.Time, opt DreamOptions) ([]DreamReport, error) {
	var out []DreamReport
	for _, ws := range Workspaces(agentDir) {
		r, err := Dream(agentDir, ws, now, opt)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	if !opt.DryRun {
		_ = os.MkdirAll(filepath.Join(agentDir, "lessons"), 0o700)
		_ = os.WriteFile(markerPath(agentDir), []byte(now.Format(time.RFC3339)), 0o600)
	}
	return out, nil
}
