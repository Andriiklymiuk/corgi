package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/timers"
)

func TestTimersTypeIntoTheSessionOrRing(t *testing.T) {
	d := trackingDaemon(t)
	t.Cleanup(d.swaps.Wait)
	var mu sync.Mutex
	var typed []string
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, target.SessionID+": "+text)
		return nil
	}
	notes := make(chan string, 4)
	d.Notify = func(_, body string) { notes <- body }
	d.Sessions.Apply(sessions.Event{Name: "Stop", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: time.Now()})

	now := time.Now()
	if _, err := timers.Add(d.Dir, timers.Timer{At: now.Add(time.Minute), Text: "check PR #7 again", Session: "s1"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := timers.Add(d.Dir, timers.Timer{At: now.Add(2 * time.Minute), Text: "stand up"}, now); err != nil {
		t.Fatal(err)
	}
	d.fireDueTimers(context.Background(), now.Add(30*time.Second))
	if len(notes) != 0 {
		t.Fatal("nothing is due yet")
	}
	d.fireDueTimers(context.Background(), now.Add(3*time.Minute))
	select {
	case body := <-notes:
		if body != "stand up" {
			t.Fatalf("ring: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reminder never rang")
	}
	d.swaps.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(typed) != 1 || typed[0] != "s1: check PR #7 again" {
		t.Fatalf("typed: %v", typed)
	}
	if left, _ := timers.List(d.Dir); len(left) != 0 {
		t.Fatalf("fired timers are gone: %+v", left)
	}
}

func TestDreamIfDue(t *testing.T) {
	d := trackingDaemon(t)
	now := time.Now()
	d.dreamIfDue(now)
	if err := lessons.Add(d.Dir, "api", lessons.Lesson{At: now.AddDate(-1, 0, 0), Source: "s", Text: "an old lesson about retries"}); err != nil {
		t.Fatal(err)
	}
	d.dreamIfDue(now)
	if left := lessons.List(d.Dir, "api"); len(left) != 0 {
		t.Fatalf("the year-old lesson was put away: %+v", left)
	}
	_ = lessons.Add(d.Dir, "api", lessons.Lesson{At: now.AddDate(-1, 0, 0), Source: "s", Text: "another old one"})
	d.dreamIfDue(now.Add(time.Hour))
	if len(lessons.List(d.Dir, "api")) != 1 {
		t.Fatal("dreaming waits a week")
	}
}

func TestAttentionCarriesTheSessionsOwnLink(t *testing.T) {
	d := trackingDaemon(t)
	pushed := make(chan push.Message, 2)
	d.Push = func(m push.Message) { pushed <- m }
	d.LinkFor = func(string) string { return "https://claude.ai/code?environment=env_x" }
	d.reportAttention(command.Command{WorkspaceID: "corgi", Detail: "Claude needs your permission", Link: "https://claude.ai/code/session_01ABC"})
	if m := <-pushed; m.Data["url"] != "https://claude.ai/code/session_01ABC" {
		t.Fatalf("pushed %v", m.Data)
	}
	d.reportAttention(command.Command{WorkspaceID: "corgi", Detail: "another prompt"})
	if m := <-pushed; m.Data["url"] != "https://claude.ai/code?environment=env_x" {
		t.Fatalf("no session link falls back to the environment: %v", m.Data)
	}
}
