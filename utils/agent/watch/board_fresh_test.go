package watch

import (
	"reflect"
	"testing"
	"time"
)

func leftRun(t *testing.T, key, url string, prs []string, text ...string) *FixLog {
	t.Helper()
	log := LoadFixLog(t.TempDir())
	now := time.Now().Add(-time.Hour)
	log.StartFor(Event{Key: key, Workspace: "w", Ref: "R", URL: url, Kind: KindPRComment}, now)
	log.Finish(key, prs, "", "", now)
	items := make([]LeftItem, 0, len(text))
	for _, s := range text {
		items = append(items, newLeft(s))
	}
	log.SetLeft(key, items)
	return log
}

func TestSweepLeftClosesWhatAMergedPullRequestSettled(t *testing.T) {
	link := "https://gitlab.com/a/b/-/merge_requests/124"
	log := leftRun(t, "k", link+"#note_1", []string{link + "/pipelines"},
		"Decide with Reynaldo: split the MR or keep it as one.",
		"File a follow-up ticket for the country select.",
		"Open an api ticket: expose deactivatedAt.",
		"Run the migration after it merges.")
	now := time.Now()
	pulls := map[string]PullStatus{"a/b!124": {State: "merged", At: now}}
	if n := log.SweepLeft(pulls, now); n != 1 {
		t.Fatalf("only the decision is over, closed %d", n)
	}
	left := log.Records()[0].Left
	if left[0].DoneBy != "merged" || !left[1].DoneAt.IsZero() || !left[2].DoneAt.IsZero() || !left[3].DoneAt.IsZero() {
		t.Fatalf("%+v", left)
	}
	if n := log.SweepLeft(pulls, now.Add(8*24*time.Hour)); n != 3 {
		t.Fatalf("a follow-up goes after a week, closed %d", n)
	}
}

func TestSweepLeftClosesEverythingOnAClosedPullRequest(t *testing.T) {
	link := "https://github.com/a/b/pull/7"
	log := leftRun(t, "k", link, []string{link}, "File a follow-up ticket.", "Decide something.")
	if n := log.SweepLeft(map[string]PullStatus{"a/b#7": {State: "closed", At: time.Now()}}, time.Now()); n != 2 {
		t.Fatalf("closed %d", n)
	}
}

func TestSweepLeftWaitsForEveryPullRequestItKnows(t *testing.T) {
	a, b := "https://github.com/a/b/pull/1", "https://github.com/a/c/pull/2"
	log := leftRun(t, "k", a, []string{a, b}, "Decide something.")
	now := time.Now()
	for name, pulls := range map[string]map[string]PullStatus{
		"one open":  {"a/b#1": {State: "merged"}, "a/c#2": {State: "open"}},
		"one draft": {"a/b#1": {State: "merged"}, "a/c#2": {State: "draft"}},
		"unknown":   {"a/b#1": {State: "merged"}},
		"none":      {},
	} {
		if n := log.SweepLeft(pulls, now); n != 0 {
			t.Fatalf("%s: closed %d", name, n)
		}
	}
	if n := log.SweepLeft(map[string]PullStatus{"a/b#1": {State: "merged"}, "a/c#2": {State: "closed"}}, now); n != 1 {
		t.Fatalf("closed %d", n)
	}
}

func TestSweepLeftLeavesARunWithNoPullRequest(t *testing.T) {
	log := leftRun(t, "k", "https://linear.app/a/issue/ABC-1", nil, "Decide something.")
	if n := log.SweepLeft(map[string]PullStatus{}, time.Now()); n != 0 {
		t.Fatalf("closed %d", n)
	}
}

func TestAPullRequestNobodyAskedAboutLatelyIsNotCalledStalled(t *testing.T) {
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	pulls := map[string]PullStatus{
		"a/b#1": {State: "open", Checks: "passing", Review: "approved", Mine: true, Link: "l", Since: old, At: now.Add(-time.Hour)},
		"a/b#2": {State: "open", Checks: "passing", Review: "approved", Mine: true, Link: "l", Since: old, At: now.Add(-3 * 24 * time.Hour)},
	}
	got := StalledPulls(pulls, now)
	if len(got) != 1 || got[0].Ref != "a/b#1" {
		t.Fatalf("%+v", got)
	}
}

func TestPullsGoneQuietAreTheLongestSilentOpenOnes(t *testing.T) {
	now := time.Now()
	at := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	pulls := map[string]PullStatus{
		"fresh":  {State: "open", Workspace: "w", At: now},
		"old":    {State: "open", Workspace: "w", At: at(100)},
		"older":  {State: "draft", Workspace: "w", At: at(200)},
		"nobody": {State: "open", At: at(300)},
		"merged": {State: "merged", Workspace: "w", At: at(400)},
		"other":  {State: "open", Workspace: "x", At: at(500)},
	}
	if got := PullsGoneQuiet(pulls, "w", 30*time.Minute, 2, now); !reflect.DeepEqual(got, []string{"nobody", "older"}) {
		t.Fatalf("%v", got)
	}
	if got := PullsGoneQuiet(pulls, "w", 30*time.Minute, 9, now); len(got) != 3 {
		t.Fatalf("%v", got)
	}
}

func settleOne(t *testing.T, state string, rec FixRecord, pulls map[string]PullStatus, now time.Time, change func(*Task)) Task {
	t.Helper()
	log := LoadTasks(t.TempDir())
	task, err := log.Add("x", "", "w", "you", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	task, _ = log.update(task.Ref(), func(k *Task) {
		k.State, k.UpdatedAt = state, now.Add(-time.Hour)
		if change != nil {
			change(k)
		}
	})
	rec.Key = task.Key()
	log.Settle([]FixRecord{rec}, pulls, nil, now)
	got, _ := log.Find(task.Ref())
	return got
}

func TestSettleMovesATaskWhenItsRunAndPullRequestSaySo(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Hour)
	link := "https://github.com/a/b/pull/7"
	cases := []struct {
		name, from, want string
		rec              FixRecord
		state            string
	}{
		{"merged from review", "Review", "Done", FixRecord{FinishedAt: done, PRs: []string{link}}, "merged"},
		{"merged from doing", "Doing", "Done", FixRecord{FinishedAt: done, PRs: []string{link}}, "merged"},
		{"closed", "Review", "Canceled", FixRecord{FinishedAt: done, PRs: []string{link}}, "closed"},
		{"open from doing", "Doing", "Review", FixRecord{FinishedAt: done, PRs: []string{link}}, "open"},
		{"open in review stays", "Review", "Review", FixRecord{FinishedAt: done, PRs: []string{link}}, "open"},
		{"no pull request", "Doing", "Review", FixRecord{FinishedAt: done}, ""},
		{"failed", "Doing", "Todo", FixRecord{FinishedAt: done, Error: "no login"}, ""},
		{"still running", "Doing", "Doing", FixRecord{}, ""},
		{"just finished", "Doing", "Doing", FixRecord{FinishedAt: now.Add(-time.Minute)}, ""},
		{"done stays", "Done", "Done", FixRecord{FinishedAt: done, PRs: []string{link}}, "closed"},
	}
	for _, c := range cases {
		pulls := map[string]PullStatus{}
		if c.state != "" {
			pulls["a/b#7"] = PullStatus{State: c.state, At: now}
		}
		if got := settleOne(t, c.from, c.rec, pulls, now, nil); got.State != c.want {
			t.Errorf("%s: %s", c.name, got.State)
		}
	}
}

func TestSettleLeavesATaskAPersonMovedOrCommentedOn(t *testing.T) {
	now := time.Now()
	rec := FixRecord{FinishedAt: now.Add(-time.Hour)}
	moved := settleOne(t, "Doing", rec, nil, now, func(k *Task) { k.UpdatedAt = now.Add(-time.Minute) })
	if moved.State != "Doing" {
		t.Fatalf("a person put it there after the run: %s", moved.State)
	}
	asked := settleOne(t, "Review", FixRecord{FinishedAt: now.Add(-time.Hour), PRs: []string{"https://github.com/a/b/pull/7"}},
		map[string]PullStatus{"a/b#7": {State: "merged"}}, now,
		func(k *Task) { k.Comments = []TaskComment{{By: "you", Text: "one more", At: now}} })
	if asked.State != "Review" {
		t.Fatalf("a comment waits for its run: %s", asked.State)
	}
}

func TestASettledTaskSaysWhyAndWakesNothing(t *testing.T) {
	now := time.Now()
	got := settleOne(t, "Doing", FixRecord{FinishedAt: now.Add(-time.Hour), Error: "no login"}, nil, now, nil)
	if got.State != "Todo" || len(got.Comments) != 1 || got.Comments[0].By != TaskAgent || got.WantsRun() {
		t.Fatalf("%+v", got)
	}
}

func TestATaskCopyKeepsItsThreadAndItsOrigin(t *testing.T) {
	now := time.Now()
	from := LoadTasks(t.TempDir())
	a, _ := from.Add("fix it", "body", "w", "you", now)
	a, _ = from.AskRun(a.Ref(), now)
	a, _ = from.Comment(a.Ref(), "you", "and this", now)
	to := LoadTasks(t.TempDir())
	if to.HasOrigin("laptop", a.ID) {
		t.Fatal("not here yet")
	}
	c, err := to.AddCopy(a, "laptop", now)
	if err != nil {
		t.Fatal(err)
	}
	if !c.WantsRun() || c.Body != "body" || len(c.Comments) != 1 || c.Origin == nil || c.Origin.ID != a.ID {
		t.Fatalf("%+v", c)
	}
	if !to.HasOrigin("LAPTOP", a.ID) {
		t.Fatal("names match in any case")
	}
	taken := to.TakenFrom("laptop")
	if len(taken) != 1 || taken[0].ID != a.ID || taken[0].Ref != c.Ref() || len(to.TakenFrom("other")) != 0 {
		t.Fatalf("%+v", taken)
	}
}

func TestMoveAwayStopsTheTaskHereOnlyOnce(t *testing.T) {
	now := time.Now()
	log := LoadTasks(t.TempDir())
	a, _ := log.Add("fix it", "", "w", "you", now)
	a, _ = log.AskRun(a.Ref(), now)
	if len(log.Wanted()) != 1 {
		t.Fatal("waits for a run")
	}
	if _, ok := log.MoveAway(a.ID, now.Add(time.Hour), "b", now); ok {
		t.Fatal("another task with that number")
	}
	moved, ok := log.MoveAway(a.ID, a.CreatedAt, "build-1 as TASK-7", now)
	if !ok || moved.State != "Canceled" || moved.WantsRun() || moved.MovedTo == "" {
		t.Fatalf("%+v", moved)
	}
	if _, again := log.MoveAway(a.ID, a.CreatedAt, "c", now); again {
		t.Fatal("once")
	}
	if len(log.Wanted()) != 0 {
		t.Fatal("no more waiting here")
	}
}

func TestMoveAwayLeavesARunningTaskItsColumn(t *testing.T) {
	now := time.Now()
	log := LoadTasks(t.TempDir())
	a, _ := log.Add("fix it", "", "w", "you", now)
	a, _ = log.Started(a.Ref(), now)
	if moved, ok := log.MoveAway(a.ID, a.CreatedAt, "b as TASK-1", now); !ok || moved.State != "Doing" {
		t.Fatalf("%+v", moved)
	}
}

func TestAThreadSentHereWakesNothing(t *testing.T) {
	now := time.Now()
	log := LoadTasks(t.TempDir())
	a, _ := log.Add("x", "", "w", "b", now)
	c, _ := log.WithThreadFrom(a.Ref(), []TaskComment{{By: "you", Text: "hi", At: now.Add(-time.Hour)}}, now)
	if c.WantsRun() || len(c.Comments) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestTaskCopiesRoundTripAndStayInTheirFolder(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTaskCopies(dir, "../evil name", TaskCopies{Fixes: []string{"w"}, Tasks: []Task{{ID: 3}}, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	c, ok := LoadTaskCopies(dir, "../evil name")
	if !ok || len(c.Tasks) != 1 || c.Fixes[0] != "w" {
		t.Fatalf("%+v", c)
	}
	if _, ok := LoadTaskCopies(dir, "nobody"); ok {
		t.Fatal("nothing kept")
	}
}
