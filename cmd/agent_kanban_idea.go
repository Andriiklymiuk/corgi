package cmd

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type CardIdea struct {
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

func ideaOfCard(c KanbanCard, taskIdea func(ref string) string, pages map[string]idea.Meta, link func(idea.Meta) string) *CardIdea {
	if c.Kind != string(watch.KindTask) {
		return nil
	}
	slug := taskIdea(c.Ref)
	page, ok := pages[slug]
	if slug == "" || !ok {
		return nil
	}
	return &CardIdea{Slug: slug, Name: firstNonEmpty(page.Name, page.Title), URL: link(page)}
}

func linkCardsToIdeas(dir string, cards []KanbanCard) {
	var tasks *watch.TaskLog
	var pages map[string]idea.Meta
	local, public := "", ""
	for i := range cards {
		if cards[i].Kind != string(watch.KindTask) {
			continue
		}
		if tasks == nil {
			tasks = watch.LoadTasks(dir)
			pages = map[string]idea.Meta{}
			for _, m := range listIdeas(dir) {
				pages[m.Slug] = m
			}
			local, public = ideaLinkBases()
		}
		if t, ok := tasks.Find(cards[i].Ref); ok {
			cards[i].Links, cards[i].Owner, cards[i].Priority, cards[i].Due = t.LinkRows(), t.Owner, t.Priority, t.Due
			cards[i].Overdue, cards[i].WaitsFor = t.Overdue(time.Now()), tasks.OpenBlockers(t)
			cards[i].Triage, cards[i].Cycle = t.Triage && !t.Asleep(time.Now()), t.Cycle
			if cards[i].Triage && cards[i].Column != ColDone {
				cards[i].Column, cards[i].Why = ColInbox, "waits for triage"
			}
			if len(cards[i].WaitsFor) > 0 && cards[i].Column != ColDone {
				cards[i].Column, cards[i].Why = ColBlocked, "waits for "+strings.Join(cards[i].WaitsFor, ", ")
			}
		}
		cards[i].Idea = ideaOfCard(cards[i], func(ref string) string {
			t, _ := tasks.Find(ref)
			return t.Idea
		}, pages, func(m idea.Meta) string { return ideaLinkFrom(local, public, m, true) })
	}
}
