package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// A running plan of three tasks, the third waiting for the first two, on
// one slot: the daemon starts one, then the next when it ends, then the
// third once both are at rest — and says the plan is done at the end.
func TestAPlanIsWorkedThroughInOrder(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 16)
	d.Notify = func(_, body string) { notes <- body }
	d.Isolate = func(dir, branch string) ([]string, error) { return []string{dir}, nil }
	spans := slowClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "notify", Slots: 1, Isolate: true}}
	d.startWatches(context.Background())

	now := time.Now()
	tasks := watch.LoadTasks(d.Dir)
	a, _ := tasks.Add("groundwork", "add the limiter package", "acme", "plan", now)
	b, _ := tasks.Add("wire the middleware", "use the limiter on /api", "acme", "plan", now)
	c, _ := tasks.Add("docs", "write it up", "acme", "plan", now)
	plans := watch.LoadPlans(d.Dir)
	p, err := plans.Add("rate limits", "limiter first", "acme", "sonnet", "test",
		[]watch.PlanTask{{ID: a.ID}, {ID: b.ID, After: []int{a.ID}}, {ID: c.ID, After: []int{a.ID, b.ID}}}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plans.SetState(p.ID, watch.PlanRunning, now); err != nil {
		t.Fatal(err)
	}

	d.advancePlans(context.Background())
	waitUntil(t, func() bool { return len(spans()) == 1 }, "one slot: one run starts")
	if task, _ := watch.LoadTasks(d.Dir).Find(a.Ref()); task.State != "Doing" {
		t.Fatalf("the started task is Doing, got %s", task.State)
	}
	// The run ends; the session put the task in Review as its prompt says.
	collectNotes(t, notes, "fixed "+a.Ref())
	if _, err := watch.LoadTasks(d.Dir).Move(a.Ref(), "Review", time.Now()); err != nil {
		t.Fatal(err)
	}
	d.advancePlans(context.Background())
	waitUntil(t, func() bool { return len(spans()) == 2 }, "the second task starts after the first is in Review")
	collectNotes(t, notes, "fixed "+b.Ref())
	_, _ = watch.LoadTasks(d.Dir).Move(b.Ref(), "Done", time.Now())
	d.advancePlans(context.Background())
	waitUntil(t, func() bool { return len(spans()) == 3 }, "the third task starts once both before it are at rest")
	collectNotes(t, notes, "fixed "+c.Ref())
	_, _ = watch.LoadTasks(d.Dir).Move(c.Ref(), "Review", time.Now())
	d.advancePlans(context.Background())
	collectNotes(t, notes, "plan P-1 finished")
	if got, _ := watch.LoadPlans(d.Dir).Find("P-1"); got.State != watch.PlanDone {
		t.Fatalf("plan state %s, want done", got.State)
	}
}

// A task whose run already ended without the session moving it is not run
// again by the plan: it waits for a person.
func TestAPlanDoesNotRetryATaskWhoseRunEnded(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 16)
	d.Notify = func(_, body string) { notes <- body }
	d.Isolate = func(dir, branch string) ([]string, error) { return []string{dir}, nil }
	spans := slowClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "notify", Slots: 1, Isolate: true}}
	d.startWatches(context.Background())
	now := time.Now()
	tasks := watch.LoadTasks(d.Dir)
	a, _ := tasks.Add("one", "do one", "acme", "plan", now)
	plans := watch.LoadPlans(d.Dir)
	p, _ := plans.Add("goal", "", "acme", "sonnet", "test", []watch.PlanTask{{ID: a.ID}}, 1, now)
	_, _ = plans.SetState(p.ID, watch.PlanRunning, now)
	d.advancePlans(context.Background())
	collectNotes(t, notes, "fixed "+a.Ref())
	waitUntil(t, func() bool { return !spans()[0].end.IsZero() }, "the run ends")
	// Back to Todo by hand, as if nothing had happened: still not rerun.
	_, _ = watch.LoadTasks(d.Dir).Move(a.Ref(), "Todo", time.Now())
	d.advancePlans(context.Background())
	time.Sleep(50 * time.Millisecond)
	if got := spans(); len(got) != 1 {
		t.Fatalf("a task whose run ended is not started again, got %d runs", len(got))
	}
}

func waitUntil(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}
