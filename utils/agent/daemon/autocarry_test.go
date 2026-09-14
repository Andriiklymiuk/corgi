package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// A session at a quota limit in a workspace with autoCarry on is carried
// once to another account with budget; without the policy, or once
// carried, nothing happens.
func TestAutoCarryMovesALimitedSessionOnce(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	var carried []string
	d.Carry = func(s sessions.Session) (string, error) {
		carried = append(carried, s.ID)
		return "work", nil
	}
	notes := make(chan string, 4)
	d.Notify = func(_, body string) { notes <- body }
	policy := Policy{Workspace: "acme", AutoCarry: true}
	d.Policy = func(sessions.Session) Policy { return policy }
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit", At: now})
	if s := d.Sessions.Sessions()[0]; s.Status != sessions.StatusLimited || s.Limit != sessions.LimitQuota {
		t.Fatalf("the fixture is a quota limit: %s %s", s.Status, s.Limit)
	}
	d.autoCarry(context.Background(), now)
	d.autoCarry(context.Background(), now.Add(time.Minute))
	if len(carried) != 1 || carried[0] != "s1" {
		t.Fatalf("carried once: %v", carried)
	}
	select {
	case body := <-notes:
		if body != "carried s1 to work: the account hit its limit" && !contains(body, "carried") {
			t.Fatalf("the notice: %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("a carry rings once")
	}

	// Off: nothing moves.
	policy.AutoCarry = false
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s2", Cwd: "/tmp/b", ClaudePID: 200, Ancestors: []int{200}, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s2", Error: "rate_limit", Message: "usage limit", At: now})
	d.autoCarry(context.Background(), now)
	if len(carried) != 1 {
		t.Fatalf("off means off: %v", carried)
	}
	// No account with budget: remembered, not asked again this episode.
	policy.AutoCarry = true
	d.Carry = func(s sessions.Session) (string, error) { carried = append(carried, "ask:"+s.ID); return "", nil }
	d.autoCarry(context.Background(), now)
	d.autoCarry(context.Background(), now)
	if len(carried) != 2 || carried[1] != "ask:s2" {
		t.Fatalf("asked once when nothing has budget: %v", carried)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
