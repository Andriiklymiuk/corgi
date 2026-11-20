package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	unreviewedAfter = 24 * time.Hour
	staleAfter      = 3 * 24 * time.Hour
)

func unreviewedWord(c KanbanCard, now time.Time) string {
	if c.Pull == nil || c.Pull.State != "open" || c.Column == ColDone {
		return ""
	}
	if c.Pull.Review != "none" && c.Pull.Review != "pending" && c.Pull.Review != "" {
		return ""
	}
	since := c.Pull.Since
	if since.IsZero() {
		since = c.Pull.At
	}
	waited := now.Sub(since)
	if since.IsZero() || waited < unreviewedAfter {
		return ""
	}
	if days := int(waited.Hours() / 24); days >= 2 {
		return fmt.Sprintf("no review for %d days", days)
	}
	return "no review for a day"
}

func markUnreviewed(cards []KanbanCard, now time.Time) {
	for i := range cards {
		cards[i].Unreviewed = unreviewedWord(cards[i], now)
	}
}

type kanbanView struct {
	name string
	what string
	keep func(c KanbanCard, now time.Time) bool
}

func cardRunning(c KanbanCard) bool {
	return (c.Fix != nil && c.Fix.Running) || (c.Session != nil && c.Session.Status == string(sessions.StatusWorking))
}

var kanbanViews = []kanbanView{
	{"needs-me", "what waits on a person: triage, blocked, a session asking, changes requested, red checks", func(c KanbanCard, _ time.Time) bool {
		if c.Column == ColDone {
			return false
		}
		asking := c.Session != nil && c.Session.Status == string(sessions.StatusNeedsInput)
		pull := c.Pull != nil && (c.Pull.Review == "changes" || c.Pull.Checks == "failing")
		return c.Triage || c.Column == ColBlocked || asking || pull
	}},
	{"unreviewed", "open pull requests nobody reviewed for a day or more", func(c KanbanCard, _ time.Time) bool { return c.Unreviewed != "" }},
	{"red", "pull requests whose checks fail", func(c KanbanCard, _ time.Time) bool {
		return c.Pull != nil && c.Pull.Checks == "failing" && c.Column != ColDone
	}},
	{"stale", "open cards nothing touched for three days", func(c KanbanCard, now time.Time) bool {
		return c.Column != ColDone && !cardRunning(c) && now.Sub(cardLastAt(c)) > staleAfter
	}},
	{"overdue", "tasks past their due date", func(c KanbanCard, _ time.Time) bool { return c.Overdue }},
	{"blocked", "cards in Blocked", func(c KanbanCard, _ time.Time) bool { return c.Column == ColBlocked }},
	{"triage", "tasks that wait for your yes", func(c KanbanCard, _ time.Time) bool { return c.Triage }},
	{"mine", "tasks this machine holds", func(c KanbanCard, _ time.Time) bool { return strings.EqualFold(c.Owner, peers.Me()) }},
	{"cycle", "tasks in the open cycle", func(c KanbanCard, _ time.Time) bool { return c.Cycle != "" && c.inOpenCycle }},
}

func kanbanViewNames() []string {
	names := make([]string, 0, len(kanbanViews))
	for _, v := range kanbanViews {
		names = append(names, v.name)
	}
	return names
}

type cardFilter struct {
	View     string `json:"view,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Priority string `json:"priority,omitempty"`
	Source   string `json:"source,omitempty"`
}

func (f cardFilter) empty() bool { return f == cardFilter{} }

func (f cardFilter) check() error {
	if f.View != "" {
		found := false
		for _, v := range kanbanViews {
			found = found || v.name == f.View
		}
		if !found {
			return fmt.Errorf("no view %q - there is %s", f.View, strings.Join(kanbanViewNames(), ", "))
		}
	}
	if _, ok := watch.TaskPriority(f.Priority); !ok {
		return fmt.Errorf("a priority is one of %s", strings.Join(watch.TaskPriorities, ", "))
	}
	return nil
}

func cardHasSource(c KanbanCard, source string) bool {
	if watch.LinkSource(c.URL) == source && c.URL != "" {
		return true
	}
	for _, l := range c.Links {
		if l.Source == source {
			return true
		}
	}
	return false
}

func (f cardFilter) apply(dir string, cards []KanbanCard, now time.Time) []KanbanCard {
	if f.empty() {
		return cards
	}
	current, open := watch.LoadCycles(dir).Current()
	kept := []KanbanCard{}
	for _, c := range cards {
		c.inOpenCycle = open && c.Cycle == current.Name
		ok := true
		for _, v := range kanbanViews {
			if v.name == f.View {
				ok = v.keep(c, now)
			}
		}
		ok = ok && (f.Owner == "" || strings.EqualFold(c.Owner, f.Owner))
		ok = ok && (f.Priority == "" || c.Priority == strings.ToLower(f.Priority))
		ok = ok && (f.Source == "" || cardHasSource(c, strings.ToLower(f.Source)))
		if ok {
			kept = append(kept, c)
		}
	}
	return kept
}

func savedViewNames(views map[string]cardFilter) []string {
	names := make([]string, 0, len(views))
	for name := range views {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

const (
	stepDone = "done"
	stepNow  = "now"
	stepBack = "back"
	stepTodo = "todo"
)

func stepState(done, now bool) string {
	switch {
	case done:
		return stepDone
	case now:
		return stepNow
	}
	return stepTodo
}

func pullSteps(c KanbanCard, merged bool) (checks, review string) {
	checks, review = stepTodo, stepTodo
	if merged {
		return stepDone, stepDone
	}
	if c.Pull == nil {
		return checks, review
	}
	checks = map[string]string{"passing": stepDone, "failing": stepBack, "pending": stepNow}[c.Pull.Checks]
	review = map[string]string{"approved": stepDone, "changes": stepBack, "pending": stepNow}[c.Pull.Review]
	return firstNonEmptyString(checks, stepTodo), firstNonEmptyString(review, stepTodo)
}

func cardTrail(c KanbanCard) []CardStep {
	if c.Fix == nil && c.Pull == nil {
		return nil
	}
	running := c.Fix != nil && c.Fix.Running
	hasPull := c.Pull != nil || (c.Fix != nil && len(c.Fix.PRs) > 0)
	merged := c.Pull != nil && c.Pull.State == "merged"
	steps := []CardStep{{"started", stepDone}, {"pull request", stepState(hasPull, running)}}
	if c.Fix != nil && c.Fix.Judge != "" && c.Fix.Judge != watch.JudgeSkipped {
		judged := stepBack
		if c.Fix.Judge == watch.JudgePassed {
			judged = stepDone
		}
		steps = append(steps, CardStep{"judge", judged})
	}
	checks, review := pullSteps(c, merged)
	return append(steps, CardStep{"checks", checks}, CardStep{"review", review}, CardStep{"merged", stepState(merged, false)})
}

func markTrails(cards []KanbanCard) {
	for i := range cards {
		cards[i].Trail = cardTrail(cards[i])
	}
}

func trailLine(steps []CardStep) string {
	marks := map[string]string{stepDone: "✓", stepNow: "…", stepBack: "✗", stepTodo: "·"}
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, marks[s.State]+" "+s.Step)
	}
	return strings.Join(parts, "  ")
}
