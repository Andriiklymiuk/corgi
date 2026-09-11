package daemon

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// A limited session is waiting on a clock. With autoContinue on, the daemon
// plans a resume from the account's reset time, types "continue" once the
// numbers say the window is back, and stops after a few tries so a limit
// that comes straight back is not fought all night.
func TestTheDaemonContinuesALimitedSessionWhenTheWindowResets(t *testing.T) {
	d := trackingDaemon(t)
	d.AutoContinue = true
	d.Sessions.Load()
	var mu sync.Mutex
	var typed []string
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, target.SessionID+":"+text+":"+strconv.FormatBool(enter))
		return nil
	}
	origJitter, origLimits := jitter, readLimits
	defer func() { jitter, readLimits = origJitter, origLimits }()
	jitter = func(time.Duration) time.Duration { return 0 }

	now := time.Now()
	reset := now.Add(20 * time.Minute)
	limits := usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 100, ResetsAt: reset}, SevenDay: usage.Window{Percent: 40}}
	readLimits = func(string) (usage.Limits, bool) { return limits, true }

	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit — resets 3pm", At: now})

	ctx := context.Background()
	d.autoContinue(ctx, now)
	s := d.Sessions.Sessions()[0]
	if s.ResumeAt.IsZero() || s.ResumeAt.Before(reset) {
		t.Fatalf("the plan is the reset plus grace, got %v for reset %v", s.ResumeAt, reset)
	}

	// The clock passes but a fresh reading says the account is still spent
	// (the week, say — a new reset ahead): wait, do not type.
	later := s.ResumeAt.Add(time.Second)
	limits.FiveHour = usage.Window{Percent: 100, ResetsAt: later.Add(time.Hour)}
	d.autoContinue(ctx, later)
	if got := d.Sessions.Sessions()[0]; !got.ResumeAt.After(later) {
		t.Fatal("numbers still full: the plan moves out, nothing is typed")
	}
	mu.Lock()
	if len(typed) != 0 {
		t.Fatalf("typed into a spent account: %v", typed)
	}
	mu.Unlock()

	// The reset lands in the cache.
	limits.FiveHour = usage.Window{Percent: 3, ResetsAt: reset.Add(5 * time.Hour)}
	at := d.Sessions.Sessions()[0].ResumeAt.Add(time.Second)
	d.autoContinue(ctx, at)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(typed) == 1 })
	mu.Lock()
	if typed[0] != "s1:continue:true" {
		t.Fatalf("typed = %v", typed)
	}
	mu.Unlock()
	if got := d.Sessions.Sessions()[0]; got.Resumes != 1 || !got.ResumeAt.IsZero() {
		t.Fatalf("one resume counted, plan cleared: %+v", got)
	}

	// The limit comes straight back, three times: the daemon stops.
	for i := 0; i < 4; i++ {
		d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: at})
		d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit", At: at})
		d.autoContinue(ctx, at)
		at = d.Sessions.Sessions()[0].ResumeAt.Add(time.Second)
		if at.IsZero() {
			at = now.Add(time.Duration(i+1) * time.Hour)
		}
		d.autoContinue(ctx, at)
	}
	d.swaps.Wait()
	mu.Lock()
	if len(typed) != maxQuotaResumes {
		t.Fatalf("bounded at %d continues, got %d", maxQuotaResumes, len(typed))
	}
	mu.Unlock()
}

func TestAutoContinueIsOffUnlessAsked(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	origLimits := readLimits
	defer func() { readLimits = origLimits }()
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FiveHour: usage.Window{Percent: 100, ResetsAt: time.Now().Add(-time.Minute)}}, true
	}
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit", At: now})
	d.autoContinue(context.Background(), now)
	if s := d.Sessions.Sessions()[0]; !s.ResumeAt.IsZero() {
		t.Fatal("off means not even a plan: it types into a terminal")
	}
}

// An overload is minutes, not hours: a growing wait from now, capped.
func TestAnOverloadWaitsMinutesNotHours(t *testing.T) {
	origJitter := jitter
	defer func() { jitter = origJitter }()
	jitter = func(time.Duration) time.Duration { return 0 }
	now := time.Now()
	s := sessions.Session{Limit: sessions.LimitOverload}
	if got := resumeTime(s, now); got.Sub(now) != overloadBackoff {
		t.Fatalf("first try after %v, got %v", overloadBackoff, got.Sub(now))
	}
	s.Resumes = 6
	if got := resumeTime(s, now); got.Sub(now) != overloadBackoffM {
		t.Fatalf("capped at %v, got %v", overloadBackoffM, got.Sub(now))
	}
}
