package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

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
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit - resets 3pm", At: now})

	ctx := context.Background()
	d.autoContinue(ctx, now)
	s := d.Sessions.Sessions()[0]
	if s.ResumeAt.IsZero() || s.ResumeAt.Before(reset) {
		t.Fatalf("the plan is the reset plus grace, got %v for reset %v", s.ResumeAt, reset)
	}

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

	for i := 0; i < 4; i++ {
		d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: at})
		d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit", At: at})
		limits.FiveHour = usage.Window{Percent: 100, ResetsAt: at.Add(time.Minute)}
		d.autoContinue(ctx, at)
		limits.FiveHour = usage.Window{Percent: 3, ResetsAt: at.Add(5 * time.Hour)}
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

func TestALimitWithNoSpentWindowIsLeftToAPerson(t *testing.T) {
	d := trackingDaemon(t)
	d.AutoContinue = true
	d.Sessions.Load()
	typed := 0
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(context.Context, sessions.FocusTarget, string, bool) error { typed++; return nil }
	origLimits := readLimits
	defer func() { readLimits = origLimits }()
	now := time.Now()
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 12, ResetsAt: now.Add(-time.Hour)}, SevenDay: usage.Window{Percent: 40, ResetsAt: now.Add(48 * time.Hour)}}, true
	}
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "You've hit your session limit · resets 2:30pm", At: now})
	for _, at := range []time.Time{now, now.Add(2 * time.Minute), now.Add(time.Hour)} {
		d.autoContinue(context.Background(), at)
	}
	d.swaps.Wait()
	if s := d.Sessions.Sessions()[0]; !s.ResumeAt.IsZero() || typed != 0 {
		t.Fatalf("a session-credit limit with fresh windows: no plan, nothing typed; got resumeAt=%v typed=%d", s.ResumeAt, typed)
	}
}

func TestAnOldUsageCacheFallsBackToTheResetInTheMessage(t *testing.T) {
	d := trackingDaemon(t)
	d.AutoContinue = true
	d.Sessions.Load()
	origJitter, origLimits := jitter, readLimits
	defer func() { jitter, readLimits = origJitter, origLimits }()
	jitter = func(time.Duration) time.Duration { return 0 }
	now := time.Date(2026, 10, 20, 20, 17, 0, 0, time.Local)
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FetchedAt: now.Add(-3 * time.Hour), FiveHour: usage.Window{Percent: 60, ResetsAt: now.Add(-time.Hour)}}, true
	}
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, TermProgram: "iTerm.app", TTY: 5, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "You've hit your session limit · resets 2:50am", At: now})
	d.autoContinue(context.Background(), now)
	want := time.Date(2026, 10, 21, 2, 50, 0, 0, time.Local).Add(resumeGrace)
	if got := d.Sessions.Sessions()[0].ResumeAt; !got.Equal(want) {
		t.Fatalf("the cache predates the limit, so the message's clock plans it: want %v, got %v", want, got)
	}
}

func TestAPanelSessionIsContinuedThroughItsWindowsExtension(t *testing.T) {
	d := trackingDaemon(t)
	d.AutoContinue = true
	d.Sessions.Load()
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	origJitter, origLimits := jitter, readLimits
	defer func() { jitter, readLimits = origJitter, origLimits }()
	jitter = func(time.Duration) time.Duration { return 0 }
	now := time.Now()
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 100, ResetsAt: now.Add(time.Minute)}}, true
	}
	d.Sessions.SetWindows([]sessions.Window{{ID: "w1", App: "Visual Studio Code", ExtHostPID: 555, PanelInput: true, UpdatedAt: now}})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 300, Ancestors: []int{300, 555, 70}, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "usage limit", At: now})

	d.autoContinue(context.Background(), now)
	at := d.Sessions.Sessions()[0].ResumeAt
	if at.IsZero() {
		t.Fatal("a panel whose window can type gets a plan")
	}
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FetchedAt: at, FiveHour: usage.Window{Percent: 2}}, true
	}
	d.autoContinue(context.Background(), at.Add(time.Second))
	d.swaps.Wait()
	data, err := os.ReadFile(filepath.Join(sessions.RevealDir(d.Dir), "w1.json"))
	if err != nil {
		t.Fatalf("the continue goes to the window as a reveal: %v", err)
	}
	var got sessions.Reveal
	if json.Unmarshal(data, &got) != nil || !got.Panel || got.SessionID != "s1" || got.Text != "continue" || !got.Enter {
		t.Fatalf("reveal = %s", data)
	}
}

func TestASessionWithNowhereToTypeIsSaidOnceNotRetried(t *testing.T) {
	d := trackingDaemon(t)
	d.AutoContinue = true
	d.Sessions.Load()
	var mu sync.Mutex
	var typed []string
	d.Raise = func(context.Context, sessions.FocusTarget) error { return nil }
	d.TypeText = func(_ context.Context, target sessions.FocusTarget, text string, enter bool) error {
		mu.Lock()
		defer mu.Unlock()
		typed = append(typed, target.SessionID+":"+text)
		return nil
	}
	origJitter, origLimits := jitter, readLimits
	defer func() { jitter, readLimits = origJitter, origLimits }()
	jitter = func(time.Duration) time.Duration { return 0 }
	now := time.Now()
	readLimits = func(string) (usage.Limits, bool) {
		return usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 100, ResetsAt: now.Add(time.Minute)}}, true
	}
	// No terminal program, no tty: the daemon's own remote-control child.
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/a", ClaudePID: 100, At: now})
	d.Sessions.Apply(sessions.Event{Name: "StopFailure", SessionID: "s1", Error: "rate_limit", Message: "You've hit your 5-hour limit", At: now})
	ctx := context.Background()
	d.autoContinue(ctx, now)
	for i := 0; i < 5; i++ {
		d.autoContinue(ctx, now.Add(2*time.Minute+time.Duration(i)*time.Minute))
	}
	s := d.Sessions.Sessions()[0]
	if s.Resumes != 0 || s.Status != sessions.StatusLimited {
		t.Fatalf("nothing to type into burns no tries: %+v", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(typed) != 0 {
		t.Fatalf("typed into thin air: %v", typed)
	}
	if n := len(d.saidNoType); n != 1 {
		t.Fatalf("said once, got %d", n)
	}
}
