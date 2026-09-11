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

// A carry leaves a handoff before it moves: a draft from git and the last
// thing said when the session wrote none, the session's own when it did.
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
