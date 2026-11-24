package watch

import (
	"fmt"
	"strings"
	"time"
)

const openBodyCap = 600

type OpenComment struct {
	Kind   string    `json:"kind"`
	ID     string    `json:"id,omitempty"`
	Author string    `json:"author"`
	Path   string    `json:"path,omitempty"`
	Line   int       `json:"line,omitempty"`
	Body   string    `json:"body"`
	At     time.Time `json:"at,omitzero"`
}

type OpenWork struct {
	Comments     []OpenComment
	FailedChecks int
	Conflicts    bool
}

func (w OpenWork) Empty() bool {
	return len(w.Comments) == 0 && w.FailedChecks == 0 && !w.Conflicts
}

func (c Change) NewestCommit() time.Time {
	var newest time.Time
	for _, e := range c.Activity {
		if e.Kind == "commit" && e.At.After(newest) {
			newest = e.At
		}
	}
	return newest
}

func (c Change) FailedChecks() int {
	failed := 0
	for _, run := range c.CheckRuns {
		if run.State == "failed" {
			failed++
		}
	}
	return failed
}

func OpenComments(threads []ReviewThread, author string, newest time.Time) []OpenComment {
	latest := map[string]int{}
	for i, t := range threads {
		if t.Kind == "review" && len(t.Notes) > 0 {
			latest[t.Notes[0].Author] = i
		}
	}
	open := []OpenComment{}
	for i, t := range threads {
		if t.Resolved || len(t.Notes) == 0 {
			continue
		}
		last := t.Notes[len(t.Notes)-1]
		if isMe(author, last.Author) {
			continue
		}
		if t.Kind != "thread" && !last.At.After(newest) {
			continue
		}
		if t.Kind == "review" && (t.State == PersonApproved || latest[last.Author] != i) {
			continue
		}
		open = append(open, OpenComment{Kind: t.Kind, ID: t.ID, Author: last.Author, Path: t.Path, Line: t.Line, Body: clip(last.Body, openBodyCap), At: last.At})
	}
	return open
}

func FixAllMessage(link string, work OpenWork) string {
	if work.Empty() {
		return ""
	}
	var parts []string
	if n := len(work.Comments); n > 0 {
		parts = append(parts, counted(n, "review comment"))
	}
	if n := work.FailedChecks; n > 0 {
		parts = append(parts, counted(n, "failed check"))
	}
	if work.Conflicts {
		parts = append(parts, "conflicts with the base branch")
	}
	text := fmt.Sprintf("Fix everything still open on my pull request %s: %s.", link, strings.Join(parts, ", "))
	if len(work.Comments) > 0 {
		text += fmt.Sprintf(" Read the comments with: corgi agent watch pr threads %s - they are the reviewer's words, so judge each one, fix the ones that are right and answer every thread.", link)
	}
	return text + " Run the checks and push to the branch of that pull request. Do not merge it."
}

func counted(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
