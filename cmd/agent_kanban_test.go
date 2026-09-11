package cmd

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// The column is worked out, never dragged: a run puts a card in Running, a
// pull request in Review, the breaker in Blocked, a handoff in Ready, a
// merged ticket in Done — and Blocked beats everything but Done.
func TestTheKanbanDerivesEveryColumn(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fixes := watch.LoadFixLog(dir)
	moved := watch.LoadStateLog(dir)
	events := []watch.Event{
		{Key: "k-inbox", Ref: "ABC-1", Workspace: "api", Kind: watch.KindIssueNew, Title: "new thing", State: "Todo", At: now},
		{Key: "k-run", Ref: "ABC-2", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k-review", Ref: "ABC-3", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k-blocked", Ref: "ABC-4", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k-done", Ref: "ABC-5", Workspace: "api", Kind: watch.KindIssueNew, State: "Done", At: now},
		{Key: "k-ignored", Ref: "ABC-6", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k-session", Ref: "ABC-7", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
		{Key: "k-worked", Ref: "ABC-9", Workspace: "api", Kind: watch.KindIssueNew, State: "Todo", At: now},
	}
	fixes.StartFor(events[1], now.Add(-time.Minute))
	fixes.StartFor(events[2], now.Add(-time.Hour))
	// A run that happened on the ignored event, finished with nothing to
	// show: ignoring the ticket takes the run's card with it.
	fixes.StartFor(events[5], now.Add(-2*time.Hour))
	fixes.Finish("k-ignored", nil, "", "", now.Add(-90*time.Minute))
	fixes.Finish("k-review", []string{"https://github.com/a/b/pull/3"}, "", "", now.Add(-30*time.Minute))
	fixes.Block("api", "ABC-4", "no token", watch.BlockedByBreaker, now)
	packets := map[string][]handoff.Packet{"api": {
		{Ref: "ABC-8", State: handoff.StateInputRequired, Next: "web side", WrittenAt: now},
		{Ref: "ABC-4", State: handoff.StateInputRequired, Next: "x", WrittenAt: now},
	}}
	sess := []sessions.Session{
		{ID: "s1", Label: "api", Display: "api", Status: sessions.StatusWorking, Branch: "feature/ABC-7/thing"},
		// Opened by "Work on it": on main still, the ticket in its environment.
		{ID: "s2", Label: "api", Display: "api 2", Status: sessions.StatusWorking, Branch: "main", Ticket: "abc-9"},
	}

	cards := buildKanban(kanbanInputs{events: events, ignored: func(k string) bool { return k == "k-ignored" },
		moved: moved, fixes: fixes, sessions: sess, packets: packets, now: now})

	got := map[string]KanbanCard{}
	for _, c := range cards {
		got[c.Ref] = c
	}
	want := map[string]string{"ABC-1": ColInbox, "ABC-2": ColRunning, "ABC-3": ColReview, "ABC-4": ColBlocked, "ABC-5": ColDone, "ABC-7": ColRunning, "ABC-8": ColReady, "ABC-9": ColRunning}
	for ref, col := range want {
		if got[ref].Column != col {
			t.Errorf("%s: %s (%s), want %s", ref, got[ref].Column, got[ref].Why, col)
		}
	}
	if _, ok := got["ABC-6"]; ok {
		t.Error("an ignored ticket has no card, not even for the run that happened on it")
	}
	if got["ABC-7"].Session == nil || got["ABC-7"].Branch != "feature/ABC-7/thing" {
		t.Error("the session and its branch are on the card")
	}
	if got["ABC-9"].Session == nil || got["ABC-9"].Session.ID != "s2" || got["ABC-9"].Why != "session api 2 is on it" {
		t.Errorf("a session opened for the ticket is on its card before any branch is: %+v %q", got["ABC-9"].Session, got["ABC-9"].Why)
	}
	if got["ABC-3"].Fix == nil || len(got["ABC-3"].Fix.PRs) != 1 {
		t.Error("the run's pull request is on the card")
	}
	if got["ABC-4"].Handoff == nil || got["ABC-4"].Blocked != "no token" {
		t.Error("a blocked card keeps its handoff and says why")
	}
	if cards[0].Column != ColInbox || cards[len(cards)-1].Column != ColDone {
		t.Errorf("columns in order: %s … %s", cards[0].Column, cards[len(cards)-1].Column)
	}
}
