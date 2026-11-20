package daemon

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAStackedPlanTaskStartsFromTheBranchesStillInReview(t *testing.T) {
	dir := t.TempDir()
	tasks := watch.LoadTasks(dir)
	now := time.Now()
	first, _ := tasks.Add("Show the banner", "", "shop", "idea", now)
	second, _ := tasks.Add("Mail at 80 percent", "", "shop", "idea", now)
	third, _ := tasks.Add("Let the owner raise the limit", "", "shop", "idea", now)
	_, _ = tasks.Move(first.Ref(), "Review", now)
	_, _ = tasks.Link(first.Ref(), []string{"https://github.com/acme/api/pull/7"}, now)
	_, _ = tasks.Move(second.Ref(), "Done", now)
	p := watch.Plan{ID: 1, Stack: true, Tasks: []watch.PlanTask{{ID: first.ID}, {ID: second.ID}, {ID: third.ID, After: []int{first.ID, second.ID}}}}
	bases := planTaskBases(p, tasks, third)
	if len(bases) != 1 || bases[0] != FixBranch(first.Ref()) {
		t.Fatalf("only the one in review is a base: %v", bases)
	}
	p.Stack = false
	if got := planTaskBases(p, tasks, third); got != nil {
		t.Fatalf("a plan that does not stack has no bases: %v", got)
	}
	note := planTaskBuildsOn(p, tasks, third)
	if !strings.Contains(note, first.Ref()+" Show the banner (review) https://github.com/acme/api/pull/7") || !strings.Contains(note, second.Ref()+" Mail at 80 percent (done)") {
		t.Fatalf("the run is told what came before: %q", note)
	}
	if planTaskBuildsOn(p, tasks, first) != "" {
		t.Fatal("a task that waits for nothing is told nothing")
	}
}

func TestTheStackNoteNamesEachRepositoryAndItsBase(t *testing.T) {
	note := StackNote([]utils.StackedRepo{{Repo: "/w/backend", Base: "corgi/task-1", Also: []string{"corgi/task-2"}}})
	for _, want := range []string{"backend starts from `corgi/task-1`", "merge `corgi/task-2`", "against that branch"} {
		if !strings.Contains(note, want) {
			t.Fatalf("missing %q in %q", want, note)
		}
	}
	if StackNote(nil) != "" {
		t.Fatal("nothing stacked, nothing said")
	}
}
