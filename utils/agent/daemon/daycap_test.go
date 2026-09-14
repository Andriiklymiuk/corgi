package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// The sweep books each session's new tokens to its workspace's day, and
// rings once when the workspace passes its day budget.
func TestSpendBooksTheDayByWorkspaceAndRingsAtTheDayCap(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	notes := make(chan string, 4)
	d.Notify = func(_, body string) { notes <- body }
	d.Policy = func(s sessions.Session) Policy { return Policy{Workspace: s.Label, DayCap: 1000} }
	// Two sweeps: the transcript grows between them.
	home := t.TempDir()
	path := filepath.Join(home, "s1.jsonl")
	row := func(in, out int) string {
		return `{"type":"assistant","message":{"usage":{"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `}}}` + "\n"
	}
	_ = os.WriteFile(path, []byte(row(300, 300)), 0o600)
	prev := transcriptOf
	transcriptOf = func(string, string, string) string { return path }
	t.Cleanup(func() { transcriptOf = prev })
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, At: now})
	live := d.Sessions.Sessions()
	d.checkSpend(live, now)
	if got := d.Ledger.TokensToday(live[0].Label, now); got != 600 {
		t.Fatalf("booked to the day: %d", got)
	}
	select {
	case b := <-notes:
		t.Fatalf("under the cap, no ring: %q", b)
	default:
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(row(300, 300))
	f.Close()
	d.checkSpend(d.Sessions.Sessions(), now)
	if got := d.Ledger.TokensToday(live[0].Label, now); got != 1200 {
		t.Fatalf("the delta is booked, not the total again: %d", got)
	}
	select {
	case b := <-notes:
		if b != "over its day budget: 1k of 1k tokens today" {
			t.Fatalf("the ring: %q", b)
		}
	case <-time.After(time.Second):
		t.Fatal("crossing the day cap rings")
	}
	// Past it already: no second ring.
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(row(100, 100))
	f.Close()
	d.checkSpend(d.Sessions.Sessions(), now)
	select {
	case b := <-notes:
		t.Fatalf("once: %q", b)
	case <-time.After(100 * time.Millisecond):
	}
	if usage.Today(now) == "" {
		t.Fatal("today is a date")
	}
}
