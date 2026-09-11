package sessions

import (
	"path/filepath"
	"testing"
	"time"
)

// A spent window and an overloaded API both arrive as rate_limit, and they
// want opposite reactions: the first is hours and a clock, the second is
// minutes and a retry. The classifier has to tell them apart from the text.
func TestALimitIsQuotaOrOverload(t *testing.T) {
	cases := []struct {
		errType, msg string
		kind         LimitKind
		reset        string
	}{
		{"rate_limit", "You've hit your usage limit. It resets at 2pm (Europe/Kiev).", LimitQuota, "2pm (Europe/Kiev)"},
		{"", "Weekly limit reached — resets Tue 08:59", LimitQuota, "Tue 08:59"},
		{"rate_limit", "API error 529: Overloaded. Please try again later.", LimitOverload, ""},
		{"", "The service is temporarily unavailable due to capacity", LimitOverload, ""},
		{"rate_limit", "rate limit", LimitQuota, ""},
	}
	for _, c := range cases {
		kind, reset, ok := ClassifyLimit(c.errType, c.msg)
		if !ok || kind != c.kind || reset != c.reset {
			t.Errorf("%q: got %s %q %v, want %s %q", c.msg, kind, reset, ok, c.kind, c.reset)
		}
	}
	if _, _, ok := ClassifyLimit("", "permission denied for Bash"); ok {
		t.Fatal("an ordinary failure is not a limit")
	}
}

func TestALimitedSessionKeepsItsKindUntilItMovesOn(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "sessions.json"), 6)
	now := time.Now()
	r.Apply(Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 1, At: now})
	r.Apply(Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit resets 3pm", At: now})
	s := r.Sessions()[0]
	if s.Status != StatusLimited || s.Limit != LimitQuota {
		t.Fatalf("limited quota, got %s %s", s.Status, s.Limit)
	}
	if !r.PlanResume("s1", now.Add(time.Hour)) {
		t.Fatal("a limited session takes a plan")
	}
	if s := r.Sessions()[0]; s.ResumeAt.IsZero() {
		t.Fatal("the plan is on the session, for the keys to show")
	}
	if got, ok := r.MarkResumed("s1"); !ok || got.Resumes != 1 || !got.ResumeAt.IsZero() {
		t.Fatalf("resumed once, plan cleared: %+v", got)
	}
	r.Apply(Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 1, At: now})
	if s := r.Sessions()[0]; s.Limit != "" || s.Resumes != 1 {
		t.Fatalf("working clears the kind but keeps the count for the episode: %+v", s)
	}
	r.Apply(Event{Name: "Stop", SessionID: "s1", At: now})
	if s := r.Sessions()[0]; s.Resumes != 0 {
		t.Fatal("a finished turn ends the episode")
	}
}
