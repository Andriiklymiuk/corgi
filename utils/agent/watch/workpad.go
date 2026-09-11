package watch

import (
	"context"
	"fmt"
	"strings"
)

// The workpad is the one comment corgi keeps on a ticket: the spec, the
// pull requests, the latest handoff, a blocker — each a section, rewritten
// in place. One comment that grows, not a trail of "corgi opened…" lines,
// and a place any machine or account can read the state of the work.

// WorkpadMarker is the first line of the comment; it is how the comment is
// found again, on Linear and on Jira alike.
const WorkpadMarker = "corgi · workpad"

// Workpad is the comment parsed: sections in the order they first appeared.
type Workpad struct {
	Order    []string
	Sections map[string]string
}

// ParseWorkpad reads a comment body; ok is false when it is not a workpad.
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

// Set replaces one section; an empty text removes it.
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

// Body is the comment as written back.
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

// UpsertWorkpad sets one section of the ticket's workpad, creating the
// comment the first time. Reads the newest comments to find it; a ticket
// with more than that many comments after the workpad gets a second one,
// which is still better than one per event.
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
