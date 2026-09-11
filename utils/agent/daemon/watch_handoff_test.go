package daemon

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
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

// With isolate on, a run gets worktrees on a branch named after the ticket
// and is told to work only there; the branch is on the fix record so undo
// can release it. Without the callback, nothing changes.
func TestAnIsolatedRunIsToldWhereToWork(t *testing.T) {
	if got := FixBranch("acme/api#42"); got != "corgi/acme-api-42" {
		t.Fatalf("branch: %s", got)
	}
	if got := FixBranch("ABC-7"); got != "corgi/abc-7" {
		t.Fatalf("branch: %s", got)
	}
	note := isolationNote("corgi/abc-7", []string{"/w/api", "/w/web"})
	for _, want := range []string{"corgi/abc-7", "/w/api", "/w/web", "do not create another branch"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note lacks %q: %s", want, note)
		}
	}
	dir := t.TempDir()
	l := watch.LoadFixLog(dir)
	l.StartFor(watch.Event{Key: "k1", Ref: "ABC-7"}, time.Now())
	l.SetBranch("k1", "corgi/abc-7")
	if got := watch.LoadFixLog(dir).RecentFixes("", 1); len(got) != 1 || got[0].Branch != "corgi/abc-7" {
		t.Fatalf("branch on the record: %+v", got)
	}
}

// The run's JSON envelope is taken apart: words to the log and the PR scan,
// numbers to the record. Output that is not the envelope is used as it came.
func TestARunsReceiptIsRecorded(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","result":"opened https://github.com/a/b/pull/9\nall green","total_cost_usd":0.4321,"num_turns":7,"usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":30000,"cache_creation_input_tokens":500}}`)
	out, r := unwrapResult(raw)
	if !r.ok || r.costUSD != 0.4321 || r.tokens != 31700 || r.turns != 7 {
		t.Fatalf("receipt: %+v", r)
	}
	if !strings.HasPrefix(string(out), "opened https://github.com/a/b/pull/9") {
		t.Fatalf("words: %s", out)
	}
	plain := []byte("just text from an older claude")
	if out, r := unwrapResult(plain); r.ok || string(out) != string(plain) {
		t.Fatal("plain output passes through")
	}
	dir := t.TempDir()
	l := watch.LoadFixLog(dir)
	l.StartFor(watch.Event{Key: "k1", Ref: "ABC-1", Workspace: "api"}, time.Now())
	l.SetCost("k1", 0.4321, 31700)
	l.StartFor(watch.Event{Key: "k2", Ref: "ABC-1", Workspace: "api"}, time.Now())
	l.SetCost("k2", 0.1, 300)
	l.StartFor(watch.Event{Key: "k3", Ref: "ABC-1", Workspace: "api"}, time.Now())
	l.Finish("k3", nil, "", "not started: 3/h cap", time.Now())
	c := watch.LoadFixLog(dir).CostFor("api", "ABC-1")
	if c.Runs != 2 || c.Tokens != 32000 || c.USD < 0.53 || c.USD > 0.54 {
		t.Fatalf("cost per ticket adds the runs that ran: %+v", c)
	}
}

// A fresh ticket is planned on the strong model, a red build fixed on the
// cheap one, and a ticket whose last run failed steps up.
func TestTheRunnerPicksAModelByKindAndSteppsUpAfterAFailure(t *testing.T) {
	spec := WatchSpec{}
	if fixModel(spec, watch.Event{Kind: watch.KindIssueNew}, 0) != "opus" || fixModel(spec, watch.Event{Kind: watch.KindCIFailed}, 0) != "sonnet" {
		t.Fatal("defaults by kind")
	}
	if fixModel(spec, watch.Event{Kind: watch.KindCIFailed}, 1) != "opus" {
		t.Fatal("a failed run steps up")
	}
	spec.Models = &config.ModelPolicy{Execute: "haiku", Escalate: "sonnet", Kinds: map[string]string{"ci.failed": "sonnet[1m]"}}
	if fixModel(spec, watch.Event{Kind: watch.KindCIFailed}, 0) != "sonnet[1m]" || fixModel(spec, watch.Event{Kind: watch.KindPRComment}, 0) != "haiku" || fixModel(spec, watch.Event{Kind: watch.KindPRComment}, 2) != "sonnet" {
		t.Fatal("the policy wins")
	}
}
