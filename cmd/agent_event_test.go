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
	ev, ok := agentEvent("PermissionRequest", "Codex", "abc", "", "Bash", `{"command":"rm -rf build"}`, "", getenv, os.Getppid())
	if !ok || ev.Name != "PermissionRequest" || ev.Agent != "codex" || ev.SessionID != "abc" || ev.Tool != "Bash" || ev.Risk != "destructive" || ev.Subject == "" || ev.TermProgram != "iTerm.app" || ev.Cwd == "" {
		t.Fatalf("%+v", ev)
	}
	ev, _ = agentEvent("PreToolUse", "gemini", "", "/tmp", "Bash", "go test ./...", "", getenv, os.Getppid())
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
