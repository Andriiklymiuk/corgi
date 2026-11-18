package cmd

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestABoardCardCutFromAnIdeaLinksToItsPage(t *testing.T) {
	pages := map[string]idea.Meta{"brewshift": {Slug: "brewshift", Name: "BrewShift", Title: "a scheduling app"}}
	ideas := map[string]string{"TASK-3": "brewshift", "TASK-4": "a-page-that-is-gone"}
	taskIdea := func(ref string) string { return ideas[ref] }
	link := func(m idea.Meta) string { return "http://127.0.0.1:8765/idea/" + m.Slug }

	got := ideaOfCard(KanbanCard{Ref: "TASK-3", Kind: string(watch.KindTask)}, taskIdea, pages, link)
	if got == nil || got.Slug != "brewshift" || got.Name != "BrewShift" || got.URL != "http://127.0.0.1:8765/idea/brewshift" {
		t.Fatalf("card idea: %+v", got)
	}
	if ideaOfCard(KanbanCard{Ref: "TASK-4", Kind: string(watch.KindTask)}, taskIdea, pages, link) != nil {
		t.Fatal("a card links to a page that was removed")
	}
	if ideaOfCard(KanbanCard{Ref: "TASK-9", Kind: string(watch.KindTask)}, taskIdea, pages, link) != nil {
		t.Fatal("a task with no idea got a link")
	}
	if ideaOfCard(KanbanCard{Ref: "TASK-3", Kind: "issue"}, taskIdea, pages, link) != nil {
		t.Fatal("a tracker ticket that shares a ref with a task got the task's idea")
	}
}

func TestTheBoardLinksATasksCardToItsIdeaEndToEnd(t *testing.T) {
	dir, _ := newIdeaHomeRig(t)
	page, err := idea.Create(dir, "", "Usage limits", "# Usage limits\n", idea.Meta{Name: "Limits"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tasks := watch.LoadTasks(dir)
	task, err := tasks.Add("Show the banner", "", "api", "idea", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Tag(task.Key(), page.Meta.Slug, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	cards := []KanbanCard{{Ref: task.Ref(), Kind: string(watch.KindTask)}, {Ref: "ABC-1", Kind: "issue"}}
	linkCardsToIdeas(dir, cards)
	if cards[0].Idea == nil || cards[0].Idea.Slug != page.Meta.Slug || cards[0].Idea.Name != "Limits" {
		t.Fatalf("the task's card: %+v", cards[0].Idea)
	}
	if cards[1].Idea != nil {
		t.Fatal("a ticket got an idea")
	}
}
