package daemon

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// A packet an earlier run left is the next run's first input — typed state
// in the prompt, with its check re-run — and a packet written during a run
// is what the run leaves behind, instead of six lines of stdout.
func TestARunReadsAndLeavesAHandoffPacket(t *testing.T) {
	dir := t.TempDir()
	spec := WatchSpec{Workspace: "api", Dir: dir}
	e := watch.Event{Kind: watch.KindIssueNew, Ref: "ABC-9", Title: "rate limits"}

	args := fixArgsWith(spec, e, "last words from stdout")
	if !strings.Contains(args[1], "last words from stdout") || strings.Contains(args[1], "left a handoff") {
		t.Fatal("no packet: the stdout tail is all there is")
	}

	p := handoff.Packet{Ref: "ABC-9", State: handoff.StateInputRequired, Done: []string{"api side"}, Remaining: []string{"web side"}, Next: "web banner"}
	if err := handoff.Write(dir, p); err != nil {
		t.Fatal(err)
	}
	args = fixArgsWith(spec, e, "last words from stdout")
	prompt := args[1]
	if !strings.Contains(prompt, "left a handoff") || !strings.Contains(prompt, "## Remaining") || strings.Contains(prompt, "last words from stdout") {
		t.Fatalf("the packet replaces the stdout tail:\n%s", prompt)
	}
	if !strings.Contains(prompt, "recorded no check") {
		t.Fatal("a packet without a verification says so")
	}
	if !strings.Contains(prompt, "corgi agent handoff --ref ABC-9") {
		t.Fatal("the run is told how to leave one")
	}

	started := time.Now().Add(-time.Minute)
	if got := runHandover(dir, "ABC-9", time.Now().Add(time.Minute), "a\nb\nc"); got != "a\nb\nc" {
		t.Fatalf("a packet older than the run is not its handover: %q", got)
	}
	if got := runHandover(dir, "ABC-9", started, "a\nb\nc"); !strings.HasPrefix(got, "handoff: input-required · 1 done · 1 remaining") {
		t.Fatalf("a packet written during the run is the handover: %q", got)
	}
}
