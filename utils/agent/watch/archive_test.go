package watch

import (
	"testing"
	"time"
)

func TestArchivedCardsStayHiddenUntilSomethingNewHappens(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := LoadArchive(dir)
	if err := l.Put(Archived{Key: "k1", Ref: "ABC-1", Column: "Inbox", Why: "quiet", LastAt: now.Add(-10 * 24 * time.Hour), At: now}); err != nil {
		t.Fatal(err)
	}
	l = LoadArchive(dir)
	if !l.Hidden("k1", now.Add(-10*24*time.Hour), now) {
		t.Fatal("the card is hidden")
	}
	if l.Hidden("k1", now.Add(-time.Hour), now) {
		t.Fatal("a newer event on the key brings it back")
	}
	if l.Hidden("k2", now, now) {
		t.Fatal("a key never archived is not hidden")
	}
	_ = l.Put(Archived{Key: "k3", Ref: "ABC-3", Column: "Done", Why: "merged", LastAt: now.Add(-time.Hour), At: now})
	if l.Hidden("k3", now.Add(-time.Hour), now) {
		t.Fatal("a Done card shows for a day after it went into the history")
	}
	if !l.Hidden("k3", now.Add(-time.Hour), now.Add(25*time.Hour)) {
		t.Fatal("then it is gone")
	}
	_ = l.Put(Archived{Key: "k1", Ref: "ABC-1", Column: "Done", Why: "merged", LastAt: now, At: now.Add(time.Minute)})
	if rows := l.Since(now.Add(-time.Second)); len(rows) != 2 || rows[0].Key != "k1" || rows[0].Why != "merged" {
		t.Fatalf("one row per key, newest first: %+v", rows)
	}
	if _, ok := l.Forget("abc-1"); !ok || l.Hidden("k1", now, now.Add(48*time.Hour)) {
		t.Fatal("forgetting puts the card back")
	}
}

func TestCostNamesWhoWorked(t *testing.T) {
	var c Cost
	c.AddWorker("", "sonnet-4-5")
	c.AddWorker("codex", "gpt-5-codex, sonnet-4-5")
	c.AddWorker("Claude", "")
	if len(c.Agents) != 2 || c.Agents[0] != "claude" || c.Agents[1] != "codex" {
		t.Fatalf("agents once each, claude when unnamed: %v", c.Agents)
	}
	if len(c.Models) != 2 || c.Models[0] != "sonnet-4-5" || c.Models[1] != "gpt-5-codex" {
		t.Fatalf("models once each: %v", c.Models)
	}
	if FinishedState("Ready to deploy") == "" || FinishedState("ready for release") == "" {
		t.Fatal("a ticket waiting on a deploy is over for the board")
	}
}

func TestARunRemembersItsModelAndTheCostNamesIt(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := LoadFixLog(dir)
	e := Event{Key: "k1", Ref: "ABC-1", Workspace: "api", Kind: KindIssueNew, At: now}
	l.StartFor(e, now)
	l.SetModel("k1", "opus-4-1")
	l.SetModel("k1", "  ")
	l.SetModel("", "x")
	l.SetModel("nope", "x")
	l.SetHarness("k1", "codex")
	l.SetCost("k1", 1.5, 3000)
	l.Finish("k1", nil, "", "", now.Add(time.Minute))
	l.StartFor(Event{Key: "k2", Ref: "ABC-1", Workspace: "api", Kind: KindIssueNew, At: now}, now)
	l.SetModel("k2", "sonnet-4-5, opus-4-1")
	l = LoadFixLog(dir)
	c := l.CostFor("api", "ABC-1")
	if c.Runs != 2 || c.USD != 1.5 || c.Tokens != 3000 {
		t.Fatalf("cost adds up: %+v", c)
	}
	if len(c.Models) != 2 || c.Models[0] != "opus-4-1" || c.Models[1] != "sonnet-4-5" || len(c.Agents) != 2 || c.Agents[0] != "codex" || c.Agents[1] != "claude" {
		t.Fatalf("who worked, once each, in order of appearance: %+v", c)
	}
	if other := l.CostFor("api", "ABC-2"); other.Runs != 0 || other.Models != nil {
		t.Fatalf("another ticket has nothing: %+v", other)
	}
	a := LoadArchive(dir)
	if a.Known("k1", now) || (*ArchiveLog)(nil).Known("k1", now) {
		t.Fatal("nothing is known yet")
	}
	_ = a.Put(Archived{Key: "k1", Ref: "ABC-1", Column: "Inbox", LastAt: now, At: now})
	if !a.Known("k1", now) || !a.Known("k1", now.Add(-time.Hour)) || a.Known("k1", now.Add(time.Hour)) {
		t.Fatal("known as it stands, not after something newer")
	}
	if err := a.Put(Archived{}); err != nil {
		t.Fatal("an empty row is ignored")
	}
}

func TestARunThatNeverReachedTheAgentSpendsNoCap(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := LoadFixLog(dir)
	start := func(key string) {
		l.StartFor(Event{Key: key, Ref: key, Workspace: "api", Kind: KindTask, At: now}, now.Add(-10*time.Minute))
	}
	start("dead")
	l.Finish("dead", nil, "", "could not create worktrees: no repository", now.Add(-10*time.Minute))
	start("cut")
	l.Finish("cut", nil, "", InterruptedReason, now.Add(-5*time.Minute))
	start("worked")
	l.SetCost("worked", 0.4, 9000)
	l.Finish("worked", nil, "", "exit status 1", now.Add(-time.Minute))
	start("running")
	if n := l.StartedSince("api", now.Add(-time.Hour)); n != 2 {
		t.Fatalf("a failure before the first token and an interrupted run are not counted; a run that spent and failed is, and so is one still going: %d", n)
	}
	for _, r := range l.Records() {
		if want := r.Key == "dead" || r.Key == "cut"; r.NeverRan() != want {
			t.Errorf("%s NeverRan = %v", r.Key, !want)
		}
	}
}

func TestTheHourlyCapCountsWorkNotRoutines(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	l.StartFor(Event{Key: "routine:api:retro:1", Ref: "routine/retro", Workspace: "api", Kind: KindRoutine, At: now}, now.Add(-10*time.Minute))
	l.SetCost("routine:api:retro:1", 0.4, 900)
	l.Finish("routine:api:retro:1", nil, "", "", now.Add(-9*time.Minute))
	l.StartFor(Event{Key: "task:1", Ref: "TASK-1", Workspace: "api", Kind: KindTask, At: now}, now.Add(-5*time.Minute))
	if n := l.WorkRunsSince("api", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("one task, the routine left out: %d", n)
	}
	if n := l.StartedSince("api", now.Add(-time.Hour)); n != 2 {
		t.Fatalf("the day and total caps still count both: %d", n)
	}
}
