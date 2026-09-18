package watch

import (
	"context"
	"fmt"
	"strings"
)

const WorkpadMarker = "corgi · workpad"

type Workpad struct {
	Order    []string
	Sections map[string]string
}

func ParseWorkpad(body string) (Workpad, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, WorkpadMarker) {
		return Workpad{}, false
	}
	w := Workpad{Sections: map[string]string{}}
	current := ""
	var buf []string
	flush := func() {
		if current != "" {
			w.Sections[current] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}
	for _, line := range strings.Split(body, "\n")[1:] {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if _, seen := w.Sections[current]; !seen {
				w.Order = append(w.Order, current)
				w.Sections[current] = ""
			}
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return w, true
}

func (w *Workpad) Set(section, text string) {
	if w.Sections == nil {
		w.Sections = map[string]string{}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		delete(w.Sections, section)
		kept := w.Order[:0]
		for _, s := range w.Order {
			if s != section {
				kept = append(kept, s)
			}
		}
		w.Order = kept
		return
	}
	if _, seen := w.Sections[section]; !seen {
		w.Order = append(w.Order, section)
	}
	w.Sections[section] = text
}

func (w Workpad) Body() string {
	var b strings.Builder
	b.WriteString(WorkpadMarker)
	b.WriteString("\n")
	for _, s := range w.Order {
		text, ok := w.Sections[s]
		if !ok || text == "" {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", s, text)
	}
	return b.String()
}

func UpsertWorkpad(ctx context.Context, w Writer, ref, section, text string) error {
	comments, err := w.RecentComments(ctx, ref, 50)
	if err != nil {
		return err
	}
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		pad, ok := ParseWorkpad(c.Body)
		if !ok {
			continue
		}
		pad.Set(section, text)
		if c.ID == "" {
			break
		}
		return w.UpdateComment(ctx, ref, c.ID, pad.Body())
	}
	pad := Workpad{}
	pad.Set(section, text)
	return w.Comment(ctx, ref, pad.Body())
}
