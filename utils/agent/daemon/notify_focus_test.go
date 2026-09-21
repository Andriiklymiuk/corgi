package daemon

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func focusNotifyDaemon(t *testing.T) (*Daemon, *[]string, *[]string) {
	t.Helper()
	d := testDaemon(t)
	var focused, linked []string
	d.NotifyFocus = func(title, body, sessionID, link string) { focused = append(focused, sessionID+"|"+link) }
	d.NotifyWithLink = func(title, body, link string) { linked = append(linked, link) }
	d.LinkFor = func(string) string { return "https://x.test/launch" }
	return d, &focused, &linked
}

func TestSessionNotificationClickFocusesItsWindow(t *testing.T) {
	d, focused, linked := focusNotifyDaemon(t)
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	s, _ := d.Sessions.Lookup("s1")

	d.notifySession("corgi agent · a", "drifting", s)

	if len(*focused) != 1 || (*focused)[0] != "s1|https://x.test/launch" {
		t.Fatalf("a session in a window rings with its id and the link for the webhook, got %v", *focused)
	}
	if len(*linked) != 0 {
		t.Fatalf("the desktop click must not open the website, got %v", *linked)
	}
}

func TestSessionWithoutAWindowFallsBackToTheLink(t *testing.T) {
	d, focused, linked := focusNotifyDaemon(t)
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, At: time.Now()})
	s, _ := d.Sessions.Lookup("s1")

	d.notifySession("corgi agent · a", "drifting", s)

	if len(*focused) != 0 || len(*linked) != 1 {
		t.Fatalf("no window to raise: the link path stays, focused=%v linked=%v", *focused, *linked)
	}
}

func TestWorkspaceNotificationFocusesTheSessionInThatFolder(t *testing.T) {
	d, focused, linked := focusNotifyDaemon(t)
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "old", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now.Add(-time.Hour)})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "new", Cwd: "/tmp/a/sub", ClaudePID: 101, TermProgram: "Apple_Terminal", TTY: 6, At: now})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "other", Cwd: "/tmp/b", ClaudePID: 102, TermProgram: "iTerm.app", TTY: 7, At: now})

	d.notifyAttention("corgi agent · a", "merged", "/tmp/a")

	if len(*focused) != 1 || (*focused)[0] != "new|https://x.test/launch" {
		t.Fatalf("the newest session under the workspace folder is the one to raise, got %v (linked %v)", *focused, *linked)
	}

	d.notifyAttention("corgi agent · c", "merged", "/tmp/c")
	if len(*linked) != 1 {
		t.Fatalf("no session in that folder: the link path stays, got focused=%v linked=%v", *focused, *linked)
	}
}

func TestExitedSessionsNeverGetFocused(t *testing.T) {
	d, focused, linked := focusNotifyDaemon(t)
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "SessionEnd", SessionID: "s1", At: now})
	s, _ := d.Sessions.LookupEnded("s1")

	d.notifySession("corgi agent · a", "over budget", s)

	if len(*focused) != 0 || len(*linked) != 1 {
		t.Fatalf("an exited session has no window, focused=%v linked=%v", *focused, *linked)
	}
}
