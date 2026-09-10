package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// Undo has to pick the right run, and say what it will do before it does it.
func TestUndoPicksTheRunAndSaysWhatItWouldDo(t *testing.T) {
	dir := todayAgentDir(t)
	now := time.Now()
	log := watch.LoadFixLog(dir)

	log.StartFor(watch.Event{Key: "jira:ABC-1", Workspace: "api", Ref: "ABC-1"}, now.Add(-2*time.Hour))
	log.Finish("jira:ABC-1", []string{"https://x/pull/1"}, "", "", now.Add(-100*time.Minute))
	log.StartFor(watch.Event{Key: "jira:ABC-2", Workspace: "api", Ref: "ABC-2"}, now.Add(-time.Hour))
	log.Finish("jira:ABC-2", nil, "no change needed", "", now.Add(-50*time.Minute))
	log.StartFor(watch.Event{Key: "jira:ABC-3", Workspace: "api", Ref: "ABC-3"}, now)

	// The newest finished run, never one still going.
	got, ok := lastUndoable(dir, "")
	if !ok || got.Ref != "ABC-2" {
		t.Fatalf("the newest finished run is the one to undo: %+v", got)
	}
	// By ref, whichever run that was.
	if got, ok := lastUndoable(dir, "ABC-1"); !ok || got.Key != "jira:ABC-1" {
		t.Fatalf("naming a ticket picks that one: %+v", got)
	}
	if _, ok := lastUndoable(dir, "NOPE-9"); ok {
		t.Fatal("a ticket with no run cannot be undone")
	}

	// A dry run says what it would do and nothing else.
	out := captureStdout(t, func() {
		reportUndo(undoPlan{Ref: "ABC-1", Workspace: "api", PRs: []string{"https://x/pull/1"}, MoveBack: "Ready"}, nil)
	})
	for _, want := range []string{"would close https://x/pull/1", "would move back to Ready", "the branch is left alone"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run is missing %q:\n%s", want, out)
		}
	}

	// Having done it, it says what it did.
	done := captureStdout(t, func() {
		reportUndo(undoPlan{Ref: "ABC-1", Workspace: "api", PRs: []string{"https://x/pull/1"},
			Closed: []string{"https://x/pull/1"}, MoveBack: "Ready", Moved: true}, nil)
	})
	if !strings.Contains(done, "closed   https://x/pull/1") || !strings.Contains(done, "moved    back to Ready") {
		t.Errorf("it has to report what happened:\n%s", done)
	}

	// A run that opened nothing and moved nothing says so rather than lying.
	empty := captureStdout(t, func() { reportUndo(undoPlan{Ref: "ABC-2", Workspace: "api"}, nil) })
	if !strings.Contains(empty, "it opened nothing") || !strings.Contains(empty, "nothing to put back") {
		t.Errorf("an empty undo is explicit:\n%s", empty)
	}

	// A problem is reported, not swallowed.
	bad := captureStdout(t, func() {
		reportUndo(undoPlan{Ref: "ABC-1", Workspace: "api"}, []string{"no gitlab token"})
	})
	if !strings.Contains(bad, "problem  no gitlab token") {
		t.Errorf("a failure has to surface:\n%s", bad)
	}
}

// The ticket has two moves, not one: In Progress when a run takes it, and
// In Review once that run has opened something. Without the second, a board
// full of "In Progress" tickets is really a board of finished work.
func TestTheTicketMovesOnWhenTheRunOpensSomething(t *testing.T) {
	dir := todayAgentDir(t)
	e := watch.Event{Key: "jira:ABC-1", Workspace: "api", Ref: "ABC-1", State: "READY TO DEV"}

	// No workspace config, so nothing is written and nothing panics.
	markDelivered(dir, "api", e, []string{"https://x/pull/1"})
	if st, ok := watch.LoadStateLog(dir).Get(e.Key); ok {
		t.Fatalf("with no review column configured nothing moves: %+v", st)
	}
	// A run that opened nothing has not delivered anything.
	markDelivered(dir, "api", e, nil)
	if _, ok := watch.LoadStateLog(dir).Get(e.Key); ok {
		t.Fatal("a run that opened nothing leaves the ticket where it is")
	}
	// An event with no ticket cannot move one.
	markDelivered(dir, "api", watch.Event{Key: "k", Workspace: "api"}, []string{"https://x/pull/1"})
	if _, ok := watch.LoadStateLog(dir).Get("k"); ok {
		t.Fatal("a pull request comment is not a ticket to move")
	}
}
