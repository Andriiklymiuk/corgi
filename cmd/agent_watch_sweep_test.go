package cmd

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type fakeMiner struct{ events []watch.Event }

func (f fakeMiner) Name() string { return "fake" }
func (f fakeMiner) Poll(context.Context, watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	return nil, nil, nil
}
func (f fakeMiner) Mine(context.Context) ([]watch.Event, error) { return f.events, nil }

func TestSweepHandsOverOnlyWhatTheDaemonNeverSaw(t *testing.T) {
	state := watch.LoadState(t.TempDir())
	now := time.Now()
	state.MarkSeen("jira:ABC-1")
	state.Fixes.StartFor(watch.Event{Key: "jira:ABC-2:h9", Workspace: "acme", Ref: "ABC-2"}, now)
	state.Fixes.Block("acme", "ABC-3", "broke twice", "breaker", now)
	src := fakeMiner{events: []watch.Event{
		{Key: "jira:ABC-1", Ref: "ABC-1", Kind: watch.KindIssueNew, Mine: true, State: "Todo"},
		{Key: "jira:ABC-2", Ref: "ABC-2", Kind: watch.KindIssueNew, Mine: true, State: "Todo"},
		{Key: "jira:ABC-3", Ref: "ABC-3", Kind: watch.KindIssueNew, Mine: true, State: "Todo"},
		{Key: "jira:ABC-4", Ref: "ABC-4", Kind: watch.KindIssueNew, Mine: true, State: "Backlog"},
		{Key: "jira:ABC-5", Ref: "ABC-5", Kind: watch.KindIssueNew, Mine: true, State: "Todo", Subtasks: []string{"ABC-6"}},
		{Key: "jira:ABC-6", Ref: "ABC-6", Kind: watch.KindIssueNew, Mine: true, State: "Todo", Parent: "ABC-5"},
		{Key: "jira:ABC-7", Ref: "ABC-7", Kind: watch.KindIssueNew, Mine: true, State: "Todo"},
	}}
	spec := daemon.WatchSpec{Workspace: "acme", Sources: []watch.Source{src}, Rules: watch.Rules{Enabled: true, States: []string{"Todo"}}}
	got, err := sweepWorkspace(context.Background(), spec, state)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, e := range got {
		refs = append(refs, e.Ref)
		if e.Workspace != "acme" {
			t.Fatalf("the event names its workspace: %+v", e)
		}
	}
	if want := "ABC-6 ABC-7"; join(refs) != want {
		t.Fatalf("seen, ran, blocked, wrong column and a parent with subtasks all stay: %v", refs)
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}
