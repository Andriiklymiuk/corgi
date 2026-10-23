package watch

import (
	"testing"
	"time"
)

func TestAPlanKeepsItsTasksInOrderAndSaysWhichCanStart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tasks := LoadTasks(dir)
	a, _ := tasks.Add("Schema", "", "acme", "you", now)
	b, _ := tasks.Add("API", "", "acme", "you", now)
	c, _ := tasks.Add("Web", "", "acme", "you", now)

	plans := LoadPlans(dir)
	if _, err := plans.Add(" ", "", "acme", "", "you", []PlanTask{{ID: a.ID}}, 1, now); err == nil {
		t.Fatal("a plan without a goal is refused")
	}
	if _, err := plans.Add("Ship it", "", "acme", "", "you", nil, 1, now); err == nil {
		t.Fatal("a plan without tasks is refused")
	}
	p, err := plans.Add(" Ship it ", " three steps ", "acme", "sonnet", "you", []PlanTask{{ID: a.ID}, {ID: b.ID, After: []int{a.ID}}, {ID: c.ID, After: []int{b.ID}}, {ID: 99}}, 0, now)
	if err != nil || p.Ref() != "P-1" || p.Goal != "Ship it" || p.Summary != "three steps" || p.Slots != 1 || p.State != PlanPlanned {
		t.Fatalf("added: %+v %v", p, err)
	}
	if !p.Has(b.ID) || p.Has(42) || len(p.TaskIDs()) != 4 {
		t.Fatalf("tasks: %v", p.TaskIDs())
	}

	pr := p.Progress(tasks)
	if pr.Todo != 3 || len(pr.Ready) != 1 || pr.Ready[0].ID != a.ID || len(pr.Missing) != 1 || pr.Missing[0] != 99 || pr.Over() {
		t.Fatalf("only the first task can start, the deleted one is missing: %+v", pr)
	}
	_, _ = tasks.Move(a.Key(), "Review", now)
	_, _ = tasks.Move(b.Key(), "Doing", now)
	pr = p.Progress(tasks)
	if pr.Review != 1 || pr.Doing != 1 || len(pr.Ready) != 0 {
		t.Fatalf("Web waits for API: %+v", pr)
	}
	_, _ = tasks.Move(b.Key(), "Done", now)
	_, _ = tasks.Move(c.Key(), "Canceled", now)
	if pr = p.Progress(tasks); !pr.Over() || pr.Done != 1 || pr.Canceled != 1 {
		t.Fatalf("nothing left to do: %+v", pr)
	}

	again := LoadPlans(dir)
	for _, arg := range []string{"P-1", "p-1", "1"} {
		if got, ok := again.Find(arg); !ok || got.ID != 1 {
			t.Errorf("%q finds the plan after a reload", arg)
		}
	}
	if _, ok := again.Find("P-x"); ok || PlanID("0") != 0 {
		t.Fatal("not a plan")
	}
	if got, err := again.SetState(1, PlanRunning, now.Add(time.Minute)); err != nil || got.State != PlanRunning || !got.UpdatedAt.After(now) {
		t.Fatalf("running: %+v %v", got, err)
	}
	if _, err := again.SetState(7, PlanRunning, now); err == nil {
		t.Fatal("no plan P-7")
	}
	_, _ = again.Add("Second", "", "acme", "", "you", []PlanTask{{ID: a.ID}}, 2, now)
	if running := again.Running(); len(running) != 1 || running[0].ID != 1 {
		t.Fatalf("running: %+v", running)
	}
	if all := again.All(); len(all) != 2 || all[0].ID != 2 {
		t.Fatalf("newest first: %+v", all)
	}
}
