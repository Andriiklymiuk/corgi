package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

func TestCarryLeavesAHandoffFirst(t *testing.T) {
	ws := t.TempDir()
	gitRepoOnBranch(t, ws, "feature/ABC-3/limits")
	s := sessions.Session{ID: "s1", Cwd: ws, Profile: "work", Status: sessions.StatusLimited,
		Summary: "the api side is done; the web banner is next", Context: &usage.Context{Percent: 40, Model: "opus"}}

	p, path := leaveCarryHandoff(ws, s, "personal")
	if path == "" || !p.Draft || p.Ref != "ABC-3" || p.Next != "the api side is done; the web banner is next" {
		t.Fatalf("draft packet: %+v %q", p, path)
	}
	if p.Where.Branch != "feature/ABC-3/limits" || p.From.Session != "s1" || p.From.Account != "work" || p.From.Model != "opus" {
		t.Fatalf("where/from: %+v", p)
	}
	if !strings.Contains(strings.Join(p.Decisions, " "), "carried from work to personal") {
		t.Fatalf("says why it moved: %v", p.Decisions)
	}

	own := handoff.Packet{Ref: "ABC-3", State: handoff.StateInputRequired, Done: []string{"api"}, Next: "web", WrittenAt: time.Now()}
	if err := handoff.Write(ws, own); err != nil {
		t.Fatal(err)
	}
	s.TurnStartedAt = time.Now().Add(-time.Hour)
	p, _ = leaveCarryHandoff(ws, s, "personal")
	if p.Draft || p.Next != "web" {
		t.Fatalf("the session's own packet wins over a draft: %+v", p)
	}

	prompt := carryPrompt(p, filepath.Join(ws, "x.md"))
	if !strings.Contains(prompt, "Continue the work on ABC-3") || !strings.Contains(prompt, "Then: web") || strings.Contains(prompt, "draft") {
		t.Fatalf("prompt: %s", prompt)
	}

	main := t.TempDir()
	gitRepoOnBranch(t, main, "main")
	if _, path := leaveCarryHandoff(main, sessions.Session{ID: "s2", Cwd: main}, "personal"); path != "" {
		t.Fatal("no ticket in the branch, no packet")
	}
}

func TestForkCommandsResumeWithAForkedSessionId(t *testing.T) {
	cmds := forkCommands("/usr/local/bin/corgi", "work", "abc-123", []string{"p1", "p2"}, 3)
	if len(cmds) != 3 {
		t.Fatalf("want 3 commands, got %d", len(cmds))
	}
	for i, c := range cmds {
		if !strings.Contains(c, "-- --resume abc-123 --fork-session") || !strings.Contains(c, "agent claude --profile work") {
			t.Fatalf("fork %d: %s", i, c)
		}
	}
	if !strings.Contains(cmds[0], "--prompt-id p1") || !strings.Contains(cmds[1], "--prompt-id p2") || strings.Contains(cmds[2], "--prompt-id") {
		t.Fatalf("prompts go to the first forks only: %v", cmds)
	}
}

func TestForkRefusesBadCounts(t *testing.T) {
	dir := t.TempDir()
	s := sessions.Session{ID: "s1", Cwd: dir, Display: "k1"}
	for _, n := range []int{0, 1, maxForks + 1} {
		if err := forkSession(dir, s, "", n, nil, "test"); err == nil {
			t.Fatalf("fork %d accepted", n)
		}
	}
	if err := forkSession(dir, s, "", 2, []string{"a", "b", "c"}, "test"); err == nil {
		t.Fatal("more prompts than forks accepted")
	}
}

func TestHandoffToAnotherHarness(t *testing.T) {
	ws := t.TempDir()
	gitRepoOnBranch(t, ws, "feature/ABC-3/limits")
	s := sessions.Session{ID: "s1", Cwd: ws, Profile: "work", Agent: "claude", Summary: "web next"}
	p, _ := leaveCarryHandoffTo(ws, s, "work", "codex")
	if p.From.Harness != "claude" || !strings.Contains(strings.Join(p.Decisions, " "), "handed from claude to codex") {
		t.Fatalf("packet says who hands to whom: %+v", p)
	}
	s.Agent = "codex"
	if p, _ := leaveCarryHandoffTo(ws, s, "work", ""); p.From.Harness != "codex" {
		t.Fatalf("from is the session's own harness: %+v", p)
	}

	c := handCommand("/usr/local/bin/corgi", "codex", "api", "work", "p1")
	if c != "/usr/local/bin/corgi agent codex --workspace api --profile work --prompt-id p1" {
		t.Fatalf("command: %s", c)
	}
	if c := handCommand("corgi", "claude", "api", "", "p1"); c != "corgi agent claude --workspace api --prompt-id p1" {
		t.Fatalf("no profile, no flag: %s", c)
	}

	if err := checkHandTarget("codex", "claude", []string{"claude"}); err == nil || !strings.Contains(err.Error(), "does not list codex") {
		t.Fatalf("a workspace that does not list codex cannot hand to it: %v", err)
	}
	if err := checkHandTarget("codex", "codex", []string{"claude", "codex"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("same harness is not a handoff: %v", err)
	}
	if err := checkHandTarget("gemini", "claude", []string{"claude", "codex"}); err == nil {
		t.Fatal("unknown harness refused")
	}
	if err := checkHandTarget("codex", "", []string{"claude", "codex"}); err != nil {
		t.Fatalf("a session with no agent on record is claude: %v", err)
	}
}
