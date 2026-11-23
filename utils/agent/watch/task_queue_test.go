package watch

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func queueHome(t *testing.T) (string, *TaskLog, time.Time) {
	t.Helper()
	dir := t.TempDir()
	return dir, LoadTasks(dir), time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
}

func addTask(t *testing.T, tasks *TaskLog, title string, now time.Time) Task {
	t.Helper()
	task, err := tasks.Add(title, "", "api", "me", now)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTheNextTaskIsTheMostUrgentReadyOne(t *testing.T) {
	dir, tasks, now := queueHome(t)
	low := addTask(t, tasks, "low", now)
	urgent := addTask(t, tasks, "urgent", now)
	waiting := addTask(t, tasks, "waits for low", now)
	triaged, _ := tasks.AddForTriage("not yet agreed", "", "api", "me", now)
	held := addTask(t, tasks, "held", now)
	snoozed := addTask(t, tasks, "snoozed", now)
	if _, err := tasks.SetPriority(urgent.Ref(), "urgent", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.SetBlockedBy(waiting.Ref(), []string{low.Ref()}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Claim(held.Ref(), "sam", false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.update(snoozed.Ref(), func(x *Task) { x.Snoozed = now.Add(time.Hour) }); err != nil {
		t.Fatal(err)
	}
	_ = triaged
	var got []string
	for i := 0; i < 4; i++ {
		task, ok, err := TakeNextTask(dir, NextQuery{Owner: "bot"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		got = append(got, task.Title)
		if task.State != "Doing" || task.Owner != "bot" || !task.LeaseUntil.Equal(now.Add(DefaultTaskLease)) {
			t.Fatalf("taken = %+v", task)
		}
	}
	if fmt.Sprint(got) != "[urgent low]" {
		t.Fatalf("taken in order %v", got)
	}
	if _, err := FinishTask(dir, low.Ref(), "bot", "done, see #8", now); err != nil {
		t.Fatal(err)
	}
	next, ok, _ := TakeNextTask(dir, NextQuery{Owner: "bot"}, now)
	if !ok || next.ID != waiting.ID {
		t.Fatalf("the task that waited is not ready once its blocker is done: %+v", next)
	}
}

func TestTagsAndWorkspaceNarrowTheQueue(t *testing.T) {
	dir, tasks, now := queueHome(t)
	plain := addTask(t, tasks, "plain", now)
	bug := addTask(t, tasks, "bug", now)
	if _, err := tasks.Tag(bug.Ref(), "", []string{"bug", "api"}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := TakeNextTask(dir, NextQuery{Owner: "bot", Tags: []string{"bug", "web"}}, now); ok {
		t.Fatalf("a task without every tag was taken")
	}
	got, ok, _ := TakeNextTask(dir, NextQuery{Owner: "bot", Tags: []string{"BUG"}}, now)
	if !ok || got.ID != bug.ID {
		t.Fatalf("got %+v", got)
	}
	if _, ok, _ := TakeNextTask(dir, NextQuery{Owner: "bot", Workspace: "web"}, now); ok {
		t.Fatalf("another workspace's task was taken")
	}
	got, ok, _ = TakeNextTask(dir, NextQuery{Owner: "bot", Workspace: "API"}, now)
	if !ok || got.ID != plain.ID {
		t.Fatalf("got %+v", got)
	}
}

func TestALeaseThatRunsOutFreesTheTaskAndOnlyItsHolderRenews(t *testing.T) {
	dir, tasks, now := queueHome(t)
	task := addTask(t, tasks, "one", now)
	if _, ok, _ := TakeNextTask(dir, NextQuery{Owner: "a", Lease: 10 * time.Minute}, now); !ok {
		t.Fatal("not taken")
	}
	if _, ok, _ := TakeNextTask(dir, NextQuery{Owner: "b"}, now.Add(9*time.Minute)); ok {
		t.Fatal("a held task was taken again before its lease ran out")
	}
	if _, err := RenewTask(dir, task.Ref(), "b", time.Hour, now); err == nil {
		t.Fatal("another name renewed it")
	}
	renewed, err := RenewTask(dir, task.Ref(), "A", 48*time.Hour, now.Add(5*time.Minute))
	if err != nil || !renewed.LeaseUntil.Equal(now.Add(5*time.Minute+MaxTaskLease)) {
		t.Fatalf("renewed = %+v err = %v", renewed.LeaseUntil, err)
	}
	again, ok, _ := TakeNextTask(dir, NextQuery{Owner: "b"}, renewed.LeaseUntil)
	if !ok || again.Owner != "b" {
		t.Fatalf("a lapsed task was not handed on: %+v", again)
	}
	if _, err := FinishTask(dir, task.Ref(), "b", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := RenewTask(dir, task.Ref(), "b", time.Hour, now); err == nil {
		t.Fatal("a finished task was renewed")
	}
	if _, err := RenewTask(dir, "TASK-99", "b", time.Hour, now); err == nil {
		t.Fatal("a missing task was renewed")
	}
}

func TestTwoAgentsAskingAtOnceNeverGetTheSameTask(t *testing.T) {
	dir, tasks, now := queueHome(t)
	for i := 0; i < 8; i++ {
		addTask(t, tasks, fmt.Sprint("t", i), now)
	}
	var mu sync.Mutex
	seen := map[int]string{}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			task, ok, err := TakeNextTask(dir, NextQuery{Owner: fmt.Sprint("agent-", n)}, now)
			if err != nil {
				t.Error(err)
				return
			}
			if !ok {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if other, dup := seen[task.ID]; dup {
				t.Errorf("TASK-%d went to %s and agent-%d", task.ID, other, n)
			}
			seen[task.ID] = fmt.Sprint("agent-", n)
		}(i)
	}
	wg.Wait()
	if len(seen) != 8 {
		t.Fatalf("taken = %d", len(seen))
	}
	for _, task := range LoadTasks(dir).Tasks {
		if task.Owner != seen[task.ID] {
			t.Errorf("TASK-%d is held by %q on disk, handed to %q", task.ID, task.Owner, seen[task.ID])
		}
	}
}

func TestFinishingKeepsTheResultWithoutAskingForARun(t *testing.T) {
	dir, tasks, now := queueHome(t)
	task := addTask(t, tasks, "one", now)
	done, err := FinishTask(dir, task.Ref(), "fixer", "fixed, review https://github.com/acme/api/pull/8", now)
	if err != nil || done.State != "Done" || done.WantsRun() || len(done.Comments) != 1 || done.Comments[0].By != "fixer" {
		t.Fatalf("done = %+v err = %v", done, err)
	}
	if _, err := FinishTask(dir, task.Ref(), "", string(make([]byte, maxTaskComment+1)), now); err == nil {
		t.Fatal("an endless result was kept")
	}
	if _, _, err := TakeNextTask(dir, NextQuery{}, now); err == nil {
		t.Fatal("a task was taken with no name")
	}
	if cleanLease(0) != DefaultTaskLease || cleanLease(time.Minute) != time.Minute {
		t.Fatal("lease bounds")
	}
}

func TestReleaseEndsTheLease(t *testing.T) {
	dir, tasks, now := queueHome(t)
	task := addTask(t, tasks, "one", now)
	if _, _, err := TakeNextTask(dir, NextQuery{Owner: "a"}, now); err != nil {
		t.Fatal(err)
	}
	freed, err := LoadTasks(dir).Release(task.Ref(), now)
	if err != nil || !freed.LeaseUntil.IsZero() || freed.LeaseOver(now) {
		t.Fatalf("freed = %+v", freed)
	}
}
