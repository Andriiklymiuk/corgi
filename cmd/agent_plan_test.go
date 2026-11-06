package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestParsePlannerAnswer(t *testing.T) {
	text := "Here is the plan:\n```json\n{\"summary\":\"limiter first\",\"tasks\":[{\"title\":\"add limiter\",\"body\":\"…\"},{\"title\":\"wire it\",\"body\":\"…\",\"after\":[1,2,9]},{\"title\":\"docs\",\"body\":\"…\",\"after\":[1]}]}\n```"
	ans, err := parsePlannerAnswer(text, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Summary != "limiter first" || len(ans.Tasks) != 2 {
		t.Fatalf("got %+v", ans)
	}
	if got := ans.Tasks[1].After; len(got) != 1 || got[0] != 1 {
		t.Fatalf("after should keep only 1 (2 is itself, 9 is nobody), got %v", got)
	}
	if _, err := parsePlannerAnswer("no json here", 6); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("prose is refused, got %v", err)
	}
	if _, err := parsePlannerAnswer(`{"tasks":[]}`, 6); err == nil {
		t.Fatal("no tasks is refused")
	}
}

func TestRepoPictureIsSmall(t *testing.T) {
	dir := t.TempDir()
	pic := repoPicture(dir)
	if !strings.Contains(pic, "Repository root") || len(pic) > 12000 {
		t.Fatalf("picture: %d bytes", len(pic))
	}
}

func TestPlanGraphShowsWhatWaitsForWhat(t *testing.T) {
	dir := t.TempDir()
	log := watch.LoadTasks(dir)
	now := time.Now()
	a, _ := log.Add("groundwork", "", "acme", "plan", now)
	b, _ := log.Add("wire it", "", "acme", "plan", now)
	p := watch.Plan{ID: 3, Goal: "rate limits", State: watch.PlanRunning, Slots: 2, Retries: 1, Tasks: []watch.PlanTask{{ID: a.ID}, {ID: b.ID, After: []int{a.ID}}}}
	text := planGraphText(p, log)
	for _, want := range []string{"P-3", "1 retries", "start → TASK-1 [Todo] groundwork\n", "TASK-1 → TASK-2 [Todo] wire it → end"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	dot := planGraphDOT(p, log)
	if !strings.Contains(dot, `digraph "P-3"`) || !strings.Contains(dot, "t1 -> t2;") {
		t.Fatalf("dot:\n%s", dot)
	}
}

func TestRetriesAreCapped(t *testing.T) {
	for in, want := range map[int]int{-1: 0, 0: 0, 2: 2, 9: 3} {
		if got := clampRetries(in); got != want {
			t.Fatalf("%d: got %d", in, got)
		}
	}
}
