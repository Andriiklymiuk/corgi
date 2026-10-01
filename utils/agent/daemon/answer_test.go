package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestDenyFromThePhoneSettlesTheRow(t *testing.T) {
	d := trackingDaemon(t)
	t.Cleanup(d.swaps.Wait)
	typed := make(chan string, 2)
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, _ sessions.FocusTarget, text string, _ bool) error {
		typed <- text
		return nil
	}
	base := sessions.Event{SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: time.Now()}
	start, perm := base, base
	start.Name = "SessionStart"
	perm.Name, perm.Tool, perm.Subject = "PermissionRequest", "Bash", "rm -rf build"
	d.Sessions.Apply(start)
	d.Sessions.Apply(perm)

	d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionAnswer, SessionID: "s1", Answer: "deny", Source: "phone"})
	if got := <-typed; got != "\x1b" {
		t.Fatalf("deny presses Esc, typed %q", got)
	}
	waitFor(t, func() bool {
		s, _ := d.Sessions.Lookup("s1")
		return s.Status == sessions.StatusDone && s.Pending == nil
	})
}

func TestAConfirmedAllowOfARiskyCommandReachesTheSession(t *testing.T) {
	d := trackingDaemon(t)
	t.Cleanup(d.swaps.Wait)
	typed := make(chan string, 2)
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, _ sessions.FocusTarget, text string, _ bool) error {
		typed <- text
		return nil
	}
	base := sessions.Event{SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: time.Now()}
	start, perm := base, base
	start.Name = "SessionStart"
	perm.Name, perm.Tool, perm.Subject = "PermissionRequest", "Bash", "rm -rf build"
	d.Sessions.Apply(start)
	d.Sessions.Apply(perm)

	d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionAnswer, SessionID: "s1", Answer: "allow", Confirmed: true, Source: "phone"})
	if got := <-typed; got != "\r" {
		t.Fatalf("a confirmed allow presses enter, typed %q", got)
	}
}
