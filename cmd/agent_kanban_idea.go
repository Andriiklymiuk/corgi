package cmd

import (
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
		cards[i].Idea = ideaOfCard(cards[i], func(ref string) string {
			t, _ := tasks.Find(ref)
			return t.Idea
		}, pages, func(m idea.Meta) string { return ideaLinkFrom(local, public, m, true) })
	}
}
