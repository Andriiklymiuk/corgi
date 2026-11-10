package cmd

import (
	"os"
	"testing"
)

func TestAnotherAgentsEventLooksLikeAHook(t *testing.T) {
	getenv := func(k string) string {
		if k == "TERM_PROGRAM" {
			return "iTerm.app"
		}
		return ""
	}
	ev, ok := agentEvent(agentHook{Name: "PermissionRequest", Agent: "Codex", Session: "abc", Tool: "Bash", Input: `{"command":"rm -rf build"}`}, getenv, os.Getppid())
	if !ok || ev.Name != "PermissionRequest" || ev.Agent != "codex" || ev.SessionID != "abc" || ev.Tool != "Bash" || ev.Risk != "destructive" || ev.Subject == "" || ev.TermProgram != "iTerm.app" || ev.Cwd == "" {
		t.Fatalf("%+v", ev)
	}
	ev, _ = agentEvent(agentHook{Name: "PreToolUse", Agent: "gemini", Cwd: "/tmp", Tool: "Bash", Input: "go test ./..."}, getenv, os.Getppid())
	if ev.Risk != "writes" || ev.Subject != "go test" || ev.Cwd != "/tmp" {
		t.Fatalf("%+v", ev)
	}
	if ev.SessionID == "" || ev.SessionID[:7] != "gemini-" {
		t.Fatalf("session from the process: %q", ev.SessionID)
	}
	if _, ok := eventNames["stop"]; !ok || eventNames["permission"] != "PermissionRequest" {
		t.Fatal("the short words")
	}
}
