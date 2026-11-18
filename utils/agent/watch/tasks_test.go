package watch

import (
	"strings"
	"testing"
	"time"
)

func TestTasksLiveOnTheBoardUntilFinished(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	l := LoadTasks(dir)
	a, err := l.Add("Meta SDK on iOS", "App Events, not the pixel", "app", "phone", now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Ref() != "TASK-1" || a.Key() != "task:1" || a.State != "Todo" {
		t.Fatalf("first task: %+v", a)
	}
	if _, err := l.Add("   ", "", "", "", now); err == nil {
		t.Fatal("a title is required")
	}
	b, _ := l.Add("Second", "", "web", "cli", now.Add(time.Minute))

	for _, arg := range []string{"TASK-1", "task:1", "1", "task-1"} {
		if got, ok := LoadTasks(dir).Find(arg); !ok || got.ID != 1 {
			t.Errorf("Find(%q) = %+v %v", arg, got, ok)
		}
	}
	if _, err := l.Move("TASK-1", "somewhere", now); err == nil {
		t.Fatal("only the board's columns")
	}
	moved, err := l.Move("TASK-1", "review", now.Add(2*time.Minute))
	if err != nil || moved.State != "Review" {
		t.Fatalf("move: %+v %v", moved, err)
	}

	events := RecentEvents(dir, 25)
	if len(events) != 2 || events[0].Key != "task:1" || events[1].Key != b.Key() {
		t.Fatalf("events: %+v", events)
	}
	if events[0].Kind != KindTask || events[0].State != "Review" || events[0].Title != "Meta SDK on iOS" || events[0].Body == "" {
		t.Fatalf("task event: %+v", events[0])
	}
	if e, ok := FindEvent(dir, "task:2"); !ok || e.Ref != "TASK-2" {
		t.Fatal("a task is found by key like any event")
	}
	if Settled(events[0], events[0].State) != "" {
		t.Fatal("review is not the end")
	}
	if _, err := l.Move("2", "Done", now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	done, _ := l.Find("2")
	if Settled(done.Event(), done.State) == "" {
		t.Fatal("done settles it")
	}
	if got := l.Events(now.Add(8 * 24 * time.Hour)); len(got) != 1 || got[0].Key != "task:1" {
		t.Fatalf("after a week: %+v", got)
	}
	if _, ok := LoadTasks(dir).Find("2"); !ok {
		t.Fatal("kept in the file")
	}
	if _, err := l.Remove("TASK-2"); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadTasks(dir).Find("2"); ok {
		t.Fatal("removed")
	}
}

func TestAPickIsRememberedUntilTheSessionShows(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := LoadPicks(dir)
	if err := p.Set("task:1", "phone", now); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadPicks(dir).Get("task:1")
	if !ok || got.By != "phone" {
		t.Fatalf("pick: %+v %v", got, ok)
	}
}

func TestACommentWakesATaskAndAnAgentReplyDoesNot(t *testing.T) {
	l := LoadTasks(t.TempDir())
	now := time.Now()
	task, err := l.Add("export", "make it work", "api", "phone", now)
	if err != nil {
		t.Fatal(err)
	}
	if task.WantsRun() {
		t.Fatal("a task for later started by itself")
	}
	if task, _ = l.AskRun(task.Ref(), now); !task.WantsRun() {
		t.Fatal("a task asked to run does not want to")
	}
	if task, _ = l.Started(task.Ref(), now); task.WantsRun() || task.State != "Doing" {
		t.Fatalf("started: %+v", task)
	}
	if task, _ = l.Comment(task.Ref(), TaskAgent, "PR is up", now.Add(time.Minute)); task.WantsRun() {
		t.Fatal("the agent woke itself: that loop never ends")
	}
	if task, _ = l.Comment(task.Ref(), "maria", "the total is still wrong", now.Add(2*time.Minute)); !task.WantsRun() {
		t.Fatal("a person's comment did not wake it")
	}
	if !strings.Contains(task.Event().Body, "- maria: the total is still wrong") || !strings.Contains(task.Event().Body, "make it work") {
		t.Fatalf("the run would not see the thread: %q", task.Event().Body)
	}
	if task, _ = l.Move(task.Ref(), "Done", now.Add(3*time.Minute)); task.WantsRun() {
		t.Fatal("a finished task woke")
	}
	if _, err := l.Comment(task.Ref(), "maria", "  ", now); err == nil {
		t.Fatal("an empty comment went through")
	}
}

func TestTheSameSourceIsAddedOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	l := LoadTasks(dir)
	first, added, err := l.AddFrom("mail:AAMk-17", "Answer the release question", "", "app", "cli", now)
	if err != nil || !added || first.From != "mail:AAMk-17" {
		t.Fatalf("first add: %+v added=%v err=%v", first, added, err)
	}
	if _, err := l.Move(first.Ref(), "Done", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	again, added, err := LoadTasks(dir).AddFrom(" mail:AAMk-17 ", "Answer it (again)", "", "app", "cli", now.Add(2*time.Hour))
	if err != nil || added || again.ID != first.ID || again.State != "Done" {
		t.Fatalf("a finished one still counts: %+v added=%v err=%v", again, added, err)
	}
	other, added, _ := l.AddFrom("", "No source", "", "app", "cli", now)
	plain, addedPlain, _ := l.AddFrom("", "No source", "", "app", "cli", now)
	if !added || !addedPlain || other.ID == plain.ID || other.From != "" {
		t.Fatalf("no source means no matching: %+v %+v", other, plain)
	}
	if len(LoadTasks(dir).Tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(LoadTasks(dir).Tasks))
	}
}

func TestACardsCostCountsTheMinutesOfFinishedRuns(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	l := &FixLog{Started: []FixRecord{
		{Workspace: "api", Ref: "ABC-1", StartedAt: start, FinishedAt: start.Add(26 * time.Minute), Tokens: 100},
		{Workspace: "api", Ref: "ABC-1", StartedAt: start.Add(time.Hour), FinishedAt: start.Add(time.Hour + 4*time.Minute + 40*time.Second), Tokens: 50},
		{Workspace: "api", Ref: "ABC-1", StartedAt: start.Add(2 * time.Hour)},
		{Workspace: "api", Ref: "ABC-2", StartedAt: start, FinishedAt: start.Add(time.Hour)},
	}}
	c := l.CostFor("api", "ABC-1")
	if c.Runs != 3 || c.Tokens != 150 || c.Minutes != 31 {
		t.Fatalf("two finished runs, 26m + 4m40s: %+v", c)
	}
}
