package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample() Packet {
	return Packet{
		Ref: "ABC-123", Tracker: "https://linear.app/x/issue/ABC-123", State: StateInputRequired,
		Where:        Where{Branch: "feature/ABC-123/rate-limit", Head: "e77d10c0000", Base: "9f1c2ab0000", Dirty: nil},
		Done:         []string{"POST /limits returns 429 with Retry-After", "regression test in api/limits_test.go"},
		Remaining:    []string{"web: banner on 429"},
		Decisions:    []string{"5h sliding window, not fixed"},
		Uncertain:    []string{"should the phone retry on its own?"},
		Verification: &Verification{Cmd: "corgi test --changed", Exit: 0, At: "e77d10c0000"},
		Next:         "web banner, then open the web PR",
		From:         From{Harness: "claude", Model: "opus", Account: "work", Session: "b2d4"},
		Budget:       Budget{Context: 62, FiveHour: 91, SevenDay: 58},
	}
}

// The packet is a file beside the workspace, ignored by git, with a
// Markdown twin; it round-trips, and the next session finds it by branch.
func TestAPacketIsWrittenReadAndFoundByBranch(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, sample()); err != nil {
		t.Fatal(err)
	}
	p, err := Read(dir, "ABC-123")
	if err != nil || p.Next != "web banner, then open the web PR" || p.WrittenAt.IsZero() {
		t.Fatalf("read back: %+v %v", p, err)
	}
	md, err := os.ReadFile(MarkdownPath(dir, "ABC-123"))
	if err != nil || !strings.Contains(string(md), "## Remaining") || !strings.Contains(string(md), "exit 0") {
		t.Fatalf("markdown twin: %s %v", md, err)
	}
	ignore, _ := os.ReadFile(filepath.Join(dir, ".corgi", "corgi_services", ".gitignore"))
	if !strings.Contains(string(ignore), "handoffs/") {
		t.Fatalf("packets are per-developer state, so they must be ignored: %s", ignore)
	}
	if got, ok := ForBranch(dir, "feature/ABC-123/rate-limit"); !ok || got.Ref != "ABC-123" {
		t.Fatal("found by branch")
	}
	if got, ok := ForBranch(dir, "fix/abc-123-typo"); !ok || got.Ref != "ABC-123" {
		t.Fatal("found by the ref in a different branch name")
	}
	if _, ok := ForBranch(dir, "main"); ok {
		t.Fatal("main has no packet")
	}
	if err := Remove(dir, "ABC-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, "ABC-123"); !os.IsNotExist(err) {
		t.Fatal("removed")
	}
}

// What must not reach the ticket, or mislead the next run.
func TestValidationRefusesSecretsPlaceholdersAndNoise(t *testing.T) {
	p := sample()
	p.Decisions = append(p.Decisions, "used LINEAR_API_KEY=lin_api_0123456789abcdef for the calls")
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("a secret in a decision: %v", err)
	}
	p = sample()
	p.Remaining = []string{"TODO figure out the rest"}
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "TODO") {
		t.Fatalf("a placeholder: %v", err)
	}
	p.Draft = true
	if err := Validate(p); err != nil {
		t.Fatalf("a draft corgi assembled may say so: %v", err)
	}
	p = sample()
	p.Done = []string{strings.Repeat("x", 400)}
	if err := Validate(p); err == nil {
		t.Fatal("an item that long is a transcript")
	}
	p = sample()
	p.State = StateBlocked
	if err := Validate(p); err == nil {
		t.Fatal("blocked needs a reason")
	}
	p.Blocked = "no GITHUB_TOKEN for the web repo"
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	p = sample()
	p.State = "stuck"
	if err := Validate(p); err == nil {
		t.Fatal("state is an enum")
	}
}

func TestRefFromBranch(t *testing.T) {
	for branch, want := range map[string]string{
		"feature/ABC-123/rate-limit": "ABC-123",
		"abc-12-slug":                "ABC-12",
		"fix/IMP-13427":              "IMP-13427",
		"main":                       "",
		"feature/no-ticket":          "",
	} {
		if got := RefFromBranch(branch); got != want {
			t.Errorf("%s: %q, want %q", branch, got, want)
		}
	}
}

// Staleness is commits since the packet's head; verification re-runs the
// packet's own command at the current head.
func TestCommitsSinceAndVerifyUseGit(t *testing.T) {
	orig := run
	defer func() { run = orig }()
	calls := map[string]string{"rev-list": "3", "rev-parse": "e77d10c0000"}
	run = func(dir string, args ...string) (string, error) { return calls[args[0]], nil }

	if n, err := CommitsSince("/x", "abc"); err != nil || n != 3 {
		t.Fatalf("commits since: %d %v", n, err)
	}
	p := sample()
	ran := ""
	v, ok := Verify("/x", p, func(dir, cmd string) (int, error) { ran = cmd; return 0, nil })
	if !ok || ran != "corgi test --changed" || v.At != "e77d10c0000" {
		t.Fatalf("verified at head: %+v %v", v, ok)
	}
	calls["rev-parse"] = "ffff0000000"
	if _, ok := Verify("/x", p, func(dir, cmd string) (int, error) { return 0, nil }); ok {
		t.Fatal("a different head is not the packet's verification")
	}
	calls["rev-parse"] = "e77d10c0000"
	if v, ok := Verify("/x", p, func(dir, cmd string) (int, error) { return 2, nil }); ok || v.Exit != 2 {
		t.Fatal("a failing check is not verified")
	}
	p.Verification = nil
	if _, ok := Verify("/x", p, nil); ok {
		t.Fatal("no command, nothing verified")
	}
}

func TestSummaryAndMarkdownSayTheState(t *testing.T) {
	p := sample()
	p.WrittenAt = time.Now()
	if s := p.Summary(); !strings.HasPrefix(s, "input-required · 2 done · 1 remaining · next: web banner") {
		t.Fatalf("summary: %s", s)
	}
	md := p.Markdown()
	for _, want := range []string{"# Handoff · ABC-123", "state: **input-required**", "feature/ABC-123/rate-limit", "e77d10c", "## Decisions", "ask before assuming", "context 62%"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}
