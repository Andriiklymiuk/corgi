package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func ticketSpec(t *testing.T) WatchSpec {
	t.Helper()
	return WatchSpec{Workspace: "acme", Dir: t.TempDir(), Rules: watch.Rules{Enabled: true, Comments: true, PRs: true}, Action: "fix"}
}

func TestATicketALiveSessionIsOnIsNotFixed(t *testing.T) {
	d := testDaemon(t)
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, Branch: "feat/hum-12-login", At: now})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s2", Cwd: "/tmp/b", ClaudePID: 101, Ticket: "HUM-13", TicketKey: "linear:HUM-13", At: now})

	for _, ref := range []string{"HUM-12", "HUM-13"} {
		why := d.stillWorthFixing(context.Background(), ticketSpec(t), watch.Event{Kind: watch.KindIssueNew, Key: "linear:" + ref, Ref: ref, State: "Ready", Mine: true})
		if !strings.Contains(why, "is on it now") {
			t.Errorf("%s: a session on the branch or the ticket means a person or a run is on it, got %q", ref, why)
		}
	}
	why := d.stillWorthFixing(context.Background(), ticketSpec(t), watch.Event{Kind: watch.KindIssueComment, Key: "linear:HUM-12:c1", Ref: "HUM-12", State: "Ready", Mine: true, Body: "please also add tests?"})
	if !strings.Contains(why, "is on it now") {
		t.Errorf("a comment on a ticket a session works is that session's, got %q", why)
	}
	if why := d.stillWorthFixing(context.Background(), ticketSpec(t), watch.Event{Kind: watch.KindIssueNew, Key: "linear:HUM-14", Ref: "HUM-14", State: "Ready", Mine: true}); why != "" {
		t.Errorf("nobody is on HUM-14, got %q", why)
	}
}

func TestAnExitedSessionDoesNotHoldItsTicket(t *testing.T) {
	d := testDaemon(t)
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, Branch: "corgi/HUM-12", At: now})
	d.Sessions.Apply(sessions.Event{Name: "SessionEnd", SessionID: "s1", At: now})

	if why := d.stillWorthFixing(context.Background(), ticketSpec(t), watch.Event{Kind: watch.KindIssueNew, Key: "linear:HUM-12", Ref: "HUM-12", State: "Ready", Mine: true}); why != "" {
		t.Fatalf("the session is gone, the ticket is free: %q", why)
	}
}

func TestAPeerSessionHoldsItsTicketToo(t *testing.T) {
	d := testDaemon(t)
	d.Sessions.SetPeers([]sessions.PeerBoard{{Name: "studio", Alive: true, Sessions: []peers.PeerSession{{ID: "p1", Label: "api", Status: "working", Branch: "feat/HUM-12"}}}})

	why := d.stillWorthFixing(context.Background(), ticketSpec(t), watch.Event{Kind: watch.KindIssueNew, Key: "linear:HUM-12", Ref: "HUM-12", State: "Ready", Mine: true})
	if !strings.Contains(why, "studio") || !strings.Contains(why, "is on it now") {
		t.Fatalf("the other laptop's session counts, got %q", why)
	}
}
