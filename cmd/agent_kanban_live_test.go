package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestCardsSayWhoWorksNowAndWhoWorkedBefore(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fixes := watch.LoadFixLog(dir)
	events := []watch.Event{
		{Key: "k1", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now.Add(-20 * 24 * time.Hour)},
		{Key: "k2", Ref: "ABC-2", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k3", Ref: "ABC-3", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now, Parent: "ABC-100", ParentTitle: "Big thing\nmore"},
		{Key: "k4", Ref: "ABC-4", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now, Parent: "ABC-100"},
		{Key: "k100", Ref: "ABC-100", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", Title: "Big thing", At: now},
	}
	fixes.StartFor(events[0], now.Add(-15*24*time.Hour))
	fixes.SetHarness("k1", "codex")
	fixes.SetModel("k1", "gpt-5-codex")
	fixes.SetCost("k1", 0.5, 1000)
	fixes.Finish("k1", nil, "", "", now.Add(-15*24*time.Hour+10*time.Minute))
	fixes.StartFor(events[1], now.Add(-time.Minute))
	fixes.SetModel("k2", "opus-4-1")
	fixes.StartFor(events[3], now.Add(-time.Hour))
	fixes.Finish("k4", []string{"https://github.com/a/b/pull/4"}, "", "", now.Add(-30*time.Minute))
	sess := []sessions.Session{{ID: "s1", Label: "api", Display: "api", Status: sessions.StatusWorking, Ticket: "ABC-3", Model: "sonnet-4-5", Agent: "claude", Tool: "Edit", Detail: "cmd/x.go", TurnStartedAt: now.Add(-2 * time.Minute)}}
	in := kanbanInputs{events: events, moved: watch.LoadStateLog(dir), fixes: fixes, sessions: sess, picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now,
		sessionTokens: func(s sessions.Session) (int64, bool) { return 500, true }}
	got := map[string]KanbanCard{}
	for _, c := range buildKanban(in) {
		got[c.Ref] = c
	}
	c1 := got["ABC-1"]
	if c1.Column != ColInbox || !strings.HasPrefix(c1.Why, "a run answered") || !strings.Contains(c1.Why, "nothing since") {
		t.Fatalf("an answered inbox row says so instead of nothing: %q", c1.Why)
	}
	if c1.Cost == nil || len(c1.Cost.Models) != 1 || c1.Cost.Models[0] != "gpt-5-codex" || len(c1.Cost.Agents) != 1 || c1.Cost.Agents[0] != "codex" {
		t.Fatalf("the run's model and agent ride on the cost: %+v", c1.Cost)
	}
	c2 := got["ABC-2"]
	if c2.Now == nil || c2.Now.Model != "opus-4-1" || c2.Now.Agent != "claude" || c2.Now.Doing == "" {
		t.Fatalf("a running fix has a live line: %+v", c2.Now)
	}
	c3 := got["ABC-3"]
	if c3.Now == nil || c3.Now.Doing != "Edit cmd/x.go" || c3.Now.Model != "sonnet-4-5" || !c3.Now.Since.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("a session's tool and model are the live line: %+v", c3.Now)
	}
	if c3.Cost == nil || c3.Cost.Sessions != 1 || len(c3.Cost.Models) != 1 || c3.Cost.Models[0] != "sonnet-4-5" {
		t.Fatalf("the session's model rides on the cost: %+v", c3.Cost)
	}
	if c3.Feature == nil || c3.Feature.Ref != "ABC-100" || c3.Feature.Title != "Big thing" {
		t.Fatalf("a subtask names its parent as the feature: %+v", c3.Feature)
	}
	if got["ABC-1"].Now != nil || got["ABC-4"].Now != nil {
		t.Fatal("only a running card has a live line")
	}
	features := kanbanFeatures(buildKanban(in))
	if len(features) != 1 || features[0].Ref != "ABC-100" || features[0].Title != "Big thing" || features[0].Total != 2 || features[0].Running != 1 || features[0].Review != 1 {
		t.Fatalf("one feature from the parent, counted by column: %+v", features)
	}
	if features[0].State != "running" || len(features[0].Waves) != 1 || len(features[0].Waves[0]) != 2 {
		t.Fatalf("subtasks with no order run side by side: %s %v", features[0].State, features[0].Waves)
	}
}

func TestPlansAreFeaturesWithAnOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	for _, title := range []string{"schema", "api", "ui", "docs"} {
		if _, err := tasks.Add(title, "", "api", "cli", now); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = tasks.Move("TASK-1", "Done", now)
	_, _ = tasks.Move("TASK-2", "Doing", now)
	plans := watch.LoadPlans(dir)
	p, err := plans.Add("Ship the thing", "", "api", "", "cli", []watch.PlanTask{{ID: 1}, {ID: 2, After: []int{1}}, {ID: 3, After: []int{1}}, {ID: 4, After: []int{2, 3}}}, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	in := kanbanInputs{events: tasks.Events(now), moved: watch.LoadStateLog(dir), fixes: watch.LoadFixLog(dir), picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now, plans: []watch.Plan{p}}
	cards := buildKanban(in)
	byRef := map[string]KanbanCard{}
	for _, c := range cards {
		byRef[c.Ref] = c
	}
	if f := byRef["TASK-4"]; f.Feature == nil || f.Feature.Ref != "P-1" || f.Feature.Title != "Ship the thing" || len(f.After) != 2 || f.After[0] != "TASK-2" {
		t.Fatalf("a plan's task names the plan and what it waits for: %+v %v", f.Feature, f.After)
	}
	features := kanbanFeatures(cards)
	if len(features) != 1 || features[0].Total != 4 || features[0].Done != 1 || features[0].Running != 1 {
		t.Fatalf("the plan is one feature: %+v", features)
	}
	waves := features[0].Waves
	if len(waves) != 3 || waves[0][0] != "TASK-1" || len(waves[1]) != 2 || waves[2][0] != "TASK-4" {
		t.Fatalf("waves: first alone, then two in parallel, then the last: %v", waves)
	}
	if features[0].State != "running" || !strings.Contains(features[0].Why, "one card runs") {
		t.Fatalf("state follows the cards: %s %s", features[0].State, features[0].Why)
	}
}

func TestArchivedCardsLeaveTheBoard(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	archive := watch.LoadArchive(dir)
	events := []watch.Event{
		{Key: "k1", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now.Add(-20 * 24 * time.Hour)},
		{Key: "k2", Ref: "ABC-2", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
	}
	_ = archive.Put(watch.Archived{Key: "k1", Ref: "ABC-1", Column: ColInbox, LastAt: now.Add(-20 * 24 * time.Hour), At: now.Add(-time.Hour)})
	_ = archive.Put(watch.Archived{Key: "k2", Ref: "ABC-2", Column: ColInbox, LastAt: now.Add(-2 * 24 * time.Hour), At: now.Add(-time.Hour)})
	in := kanbanInputs{events: events, moved: watch.LoadStateLog(dir), fixes: watch.LoadFixLog(dir), picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now, archive: archive}
	cards := buildKanban(in)
	if len(cards) != 1 || cards[0].Ref != "ABC-2" {
		t.Fatalf("the archived card is gone, the one with a newer event is back: %+v", cards)
	}
}

func TestReconcileLetsTheQuietCardsGo(t *testing.T) {
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	cards := []KanbanCard{
		{Key: "answered", Ref: "A", Column: ColInbox, UpdatedAt: old, Fix: &CardFix{StartedAt: old}},
		{Key: "fresh", Ref: "B", Column: ColInbox, UpdatedAt: now.Add(-time.Hour), Fix: &CardFix{StartedAt: now.Add(-time.Hour)}},
		{Key: "untouched", Ref: "C", Column: ColInbox, UpdatedAt: now.Add(-15 * 24 * time.Hour)},
		{Key: "untouched-young", Ref: "D", Column: ColInbox, UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		{Key: "running", Ref: "E", Column: ColRunning, UpdatedAt: old, Fix: &CardFix{Running: true, StartedAt: old}},
		{Key: "session", Ref: "F", Column: ColInbox, UpdatedAt: old, Session: &CardSess{ID: "s"}},
		{Key: "done", Ref: "G", Column: ColDone, Why: "merged", UpdatedAt: now.Add(-time.Minute), Cost: &CardCost{Tokens: 10, Models: []string{"opus-4-1"}}},
		{Key: "review-open", Ref: "H", Column: ColReview, UpdatedAt: old, Pull: &watch.PullStatus{State: "open"}},
		{Key: "review-closed", Ref: "I", Column: ColReview, UpdatedAt: old, Pull: &watch.PullStatus{State: "closed"}},
		{Key: "blocked", Ref: "J", Column: ColBlocked, UpdatedAt: old},
		{Key: "picked", Ref: "K", Column: ColInbox, UpdatedAt: old, Fix: &CardFix{StartedAt: old}, Picked: &CardPick{At: now.Add(-time.Minute)}},
		{Key: "handoff", Ref: "L", Column: ColReady, UpdatedAt: now.Add(-15 * 24 * time.Hour), Handoff: &CardHand{State: "input_required"}},
	}
	archive := watch.LoadArchive(t.TempDir())
	lines := reconcileKanban(cards, archive, 0, now, false)
	left := map[string]bool{}
	for _, l := range lines {
		left[strings.Fields(l)[0]] = true
	}
	for _, ref := range []string{"A", "C", "G", "I"} {
		if !left[ref] {
			t.Errorf("%s leaves the board: %v", ref, lines)
		}
	}
	for _, ref := range []string{"B", "D", "E", "F", "H", "J", "K", "L"} {
		if left[ref] {
			t.Errorf("%s stays: %v", ref, lines)
		}
	}
	rows := archive.Since(now.Add(-time.Second))
	if len(rows) != 4 {
		t.Fatalf("every card that left is in the history: %d", len(rows))
	}
	for _, r := range rows {
		if r.Ref == "G" && (r.Cost == nil || r.Cost.Models[0] != "opus-4-1" || r.Why != "merged") {
			t.Fatalf("a Done card goes into the history with its cost and why: %+v", r)
		}
	}
	if archive.Hidden("done", now.Add(-time.Minute), now) {
		t.Fatal("a Done card still shows today")
	}
	if n := len(reconcileKanban(cards, archive, 0, now, true)); n != 0 {
		t.Fatalf("what is in the history already is not named again: %d", n)
	}
	if n := len(reconcileKanban(cards, watch.LoadArchive(t.TempDir()), 0, now, true)); n != 4 {
		t.Fatalf("a dry run on a fresh history names the same cards: %d", n)
	}
}

func TestHistoryTellsWhatHappened(t *testing.T) {
	now := time.Now()
	since := now.Add(-7 * 24 * time.Hour)
	tasks := watch.LoadTasks(t.TempDir())
	_, _ = tasks.Add("old task", "", "api", "cli", now.Add(-time.Hour))
	_, _ = tasks.Move("TASK-1", "Done", now.Add(-time.Hour))
	_, _ = tasks.Comment("TASK-1", watch.TaskAgent, "Its pull request merged.", now.Add(-time.Hour))
	in := historyInputs{
		runs: []watch.FixRecord{
			{Key: "k1", Workspace: "api", Ref: "ABC-1", Title: "thing", StartedAt: now.Add(-2 * time.Hour), FinishedAt: now.Add(-90 * time.Minute), PRs: []string{"https://github.com/a/b/pull/1"}, CostUSD: 1, Tokens: 2000, Harness: "codex", Model: "gpt-5-codex"},
			{Key: "k2", Workspace: "api", Ref: "routine/lessons", StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour)},
			{Key: "k3", Workspace: "api", Ref: "ABC-3", StartedAt: now.Add(-time.Hour)},
			{Key: "k4", Workspace: "api", Ref: "ABC-4", StartedAt: since.Add(-time.Hour), FinishedAt: since.Add(-time.Hour)},
		},
		archive: []watch.Archived{{Key: "k5", Ref: "ABC-5", Workspace: "api", Column: ColInbox, Why: "quiet", At: now.Add(-time.Minute)}, {Key: "k6", Ref: "ABC-6", Workspace: "api", Column: ColDone, Why: "merged", At: now.Add(-time.Minute)}},
		pulls:   map[string]watch.PullStatus{"a/b#1": {State: "merged", At: now.Add(-time.Hour), Workspace: "api", Link: "https://github.com/a/b/pull/1"}, "a/b#2": {State: "open", At: now}},
		tasks:   tasks.Tasks,
		since:   since,
	}
	rows := buildHistory(in)
	kinds := map[string]HistoryEntry{}
	for _, r := range rows {
		kinds[r.Ref] = r
	}
	if r := kinds["ABC-1"]; r.Kind != HistRun || r.Cost == nil || r.Cost.Models[0] != "gpt-5-codex" || r.Cost.Agents[0] != "codex" || r.Took == "" {
		t.Fatalf("a finished run with its model, agent and how long it took: %+v", r)
	}
	if _, ok := kinds["routine/lessons"]; ok {
		t.Fatal("routines are not history")
	}
	if _, ok := kinds["ABC-3"]; ok {
		t.Fatal("a run still going is not history")
	}
	if _, ok := kinds["ABC-4"]; ok {
		t.Fatal("older than since is left out")
	}
	if kinds["ABC-5"].Kind != HistLeft || kinds["ABC-6"].Kind != HistDone {
		t.Fatalf("archived cards: %s %s", kinds["ABC-5"].Kind, kinds["ABC-6"].Kind)
	}
	if kinds["a/b#1"].Kind != HistMerged || kinds["a/b#2"].Ref != "" {
		t.Fatalf("merged pull requests only: %+v", kinds["a/b#1"])
	}
	if r := kinds["TASK-1"]; r.Kind != HistDone || r.Why != "Its pull request merged." {
		t.Fatalf("a finished task with the agent's last word: %+v", r)
	}
	if len(rows) < 2 || rows[0].At.Before(rows[1].At) {
		t.Fatal("newest first")
	}
}

func TestAFeatureStateFollowsItsCards(t *testing.T) {
	cases := []struct {
		f     KanbanFeature
		state string
		why   string
	}{
		{KanbanFeature{Total: 3, Blocked: 1, Running: 1}, "blocked", "1 card is blocked"},
		{KanbanFeature{Total: 2, Done: 2}, "done", "every card is done"},
		{KanbanFeature{Total: 4, Done: 1, Running: 2}, "running", "2 cards run in parallel · 3 left"},
		{KanbanFeature{Total: 3, Done: 1, Running: 1}, "running", "one card runs · 2 left"},
		{KanbanFeature{Total: 3, Done: 1, Review: 2}, "review", "2 cards in review, nothing else left"},
		{KanbanFeature{Total: 4, Done: 1, Review: 1}, "review", "1 card in review · 2 still to do"},
		{KanbanFeature{Total: 3, Done: 1}, "waiting", "2 cards to do · 1 done"},
	}
	for _, c := range cases {
		state, why := featureState(c.f)
		if state != c.state || why != c.why {
			t.Errorf("%+v: %s %q, want %s %q", c.f, state, why, c.state, c.why)
		}
	}
	if line := featureLine(KanbanFeature{Ref: "P-1", State: "waiting", Why: "w", Waves: [][]string{{"TASK-1"}, {"TASK-2", "TASK-3"}}}); !strings.HasSuffix(line, "TASK-1 → TASK-2 + TASK-3") {
		t.Fatalf("the order on the line: %q", line)
	}
}

func TestAFailedRunOnATaskBackInTodoSaysItFailed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	_, _ = tasks.Add("x", "", "corgi", "cli", now)
	fixes := watch.LoadFixLog(dir)
	fixes.StartFor(watch.Event{Key: "task:1", Ref: "TASK-1", Workspace: "corgi", Kind: watch.KindTask, At: now}, now.Add(-time.Hour))
	fixes.Finish("task:1", nil, "", "could not create worktrees: x", now.Add(-time.Hour))
	cards := buildKanban(kanbanInputs{events: tasks.Events(now), moved: watch.LoadStateLog(dir), fixes: fixes, picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now})
	if len(cards) != 1 || !strings.HasPrefix(cards[0].Why, "the last run failed") {
		t.Fatalf("a failure is not \"a run answered\": %+v", cards)
	}
}

func TestTheNewestRunSpeaksForTheCard(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	_, _ = tasks.Add("x", "", "corgi", "cli", now)
	_, _ = tasks.Started("TASK-1", now)
	fixes := watch.LoadFixLog(dir)
	e := watch.Event{Key: "task:1", Ref: "TASK-1", Workspace: "corgi", Kind: watch.KindTask, At: now}
	fixes.StartFor(e, now.Add(-time.Hour))
	fixes.Finish("task:1", []string{"https://github.com/a/b/pull/9"}, "", "could not create worktrees: x", now.Add(-time.Hour))
	fixes.StartFor(e, now.Add(-time.Minute))
	fixes.SetModel("task:1", "opus")
	cards := buildKanban(kanbanInputs{events: tasks.Events(now), moved: watch.LoadStateLog(dir), fixes: fixes, picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now})
	c := cards[0]
	if c.Column != ColRunning || c.Now == nil || c.Now.Model != "opus" || strings.Contains(c.Why, "failed") || !c.Fix.Running {
		t.Fatalf("the run going now is the card's state, not the one that failed before it: %s %q %+v", c.Column, c.Why, c.Now)
	}
	if len(c.Fix.PRs) != 1 {
		t.Fatalf("an older run's pull request is kept: %+v", c.Fix)
	}
}
