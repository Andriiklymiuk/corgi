package supervisor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProcess is a scripted remote-control run. With exitNow it returns
// immediately; otherwise it runs until uptime elapses or Stop is called, which
// is what a real supervised process does.
type fakeProcess struct {
	pid     int
	code    int
	output  string
	uptime  time.Duration
	exitNow bool
	stopped chan struct{}
	once    sync.Once
}

func (f *fakeProcess) Pid() int { return f.pid }

func (f *fakeProcess) Wait() (int, string) {
	if f.exitNow {
		return f.code, f.output
	}
	if f.uptime > 0 {
		select {
		case <-time.After(f.uptime):
		case <-f.stopped:
		}
		return f.code, f.output
	}
	<-f.stopped
	return f.code, f.output
}

func (f *fakeProcess) Stop() { f.once.Do(func() { close(f.stopped) }) }

// scriptedStarter hands out the given runs in order, then blocks until the
// context is cancelled so the loop cannot spin past the script.
func scriptedStarter(runs ...*fakeProcess) (Starter, *int) {
	var mu sync.Mutex
	calls := 0
	return func(ctx context.Context, _ SpawnConfig) (Process, error) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i < len(runs) {
			runs[i].stopped = make(chan struct{})
			return runs[i], nil
		}
		blocked := &fakeProcess{pid: 9000 + i, stopped: make(chan struct{})}
		go func() {
			<-ctx.Done()
			blocked.Stop()
		}()
		return blocked, nil
	}, &calls
}

func testRunner(t *testing.T, start Starter) *Runner {
	t.Helper()
	r := NewRunner(SpawnConfig{WorkspaceID: "acme", Dir: "/tmp/acme", WakeLock: WakeLockOff}, start, NewWakeLock(WakeLockOff))
	r.Sleep = func(context.Context, time.Duration) {} // no real backoff in tests
	r.HealthyAfter = time.Millisecond                 // a run counts as healthy fast
	return r
}

func TestRunnerRestartsAfterNetworkTimeout(t *testing.T) {
	// A healthy run that exits cleanly is the documented ~10 minute timeout.
	start, calls := scriptedStarter(
		&fakeProcess{pid: 1, code: 0, uptime: 20 * time.Millisecond},
	)
	r := testRunner(t, start)

	var notified []string
	var mu sync.Mutex
	r.Notify = func(_, body string) {
		mu.Lock()
		notified = append(notified, body)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(notified) > 0 })
	cancel()
	<-done

	if *calls < 2 {
		t.Errorf("started %d processes, want at least 2 — a network timeout must be restarted", *calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notified) == 0 {
		t.Fatal("a restart must notify")
	}
	if !strings.Contains(notified[0], "previous session ended") {
		t.Errorf("notification %q should say the previous session ended, since the new one starts clean", notified[0])
	}
}

func TestRunnerStopsOnAuthFailureWithoutLooping(t *testing.T) {
	start, calls := scriptedStarter(
		&fakeProcess{pid: 1, code: 1, exitNow: true, output: "Remote Control requires a claude.ai subscription"},
	)
	r := testRunner(t, start)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v, want nil after disabling", err)
	}

	if *calls != 1 {
		t.Errorf("started %d processes, want 1 — retrying cannot produce credentials", *calls)
	}
	if state := r.State(); !state.Disabled {
		t.Error("the workspace must be marked disabled so doctor can explain it")
	}
}

func TestRunnerGivesUpAfterRepeatedStartupFailures(t *testing.T) {
	runs := make([]*fakeProcess, MaxStartupFailures+3)
	for i := range runs {
		runs[i] = &fakeProcess{pid: i + 1, code: 1, exitNow: true}
	}
	start, calls := scriptedStarter(runs...)
	r := testRunner(t, start)

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	if *calls > MaxStartupFailures {
		t.Errorf("started %d processes, want at most %d — a crash loop must stop", *calls, MaxStartupFailures)
	}
	if !r.State().Disabled {
		t.Error("persistent startup failure must disable the workspace")
	}
}

func TestRunnerStopsWhenContextCancelled(t *testing.T) {
	start, _ := scriptedStarter()
	r := testRunner(t, start)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	waitFor(t, func() bool { return r.State().Running })
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after cancellation — the supervisor would block shutdown")
	}
	if r.State().Running {
		t.Error("state must not still claim to be running after shutdown")
	}
}

func TestRunnerReleasesWakeLockOnReturn(t *testing.T) {
	start, _ := scriptedStarter()
	lock := NewWakeLock(WakeLockSession)
	lock.startFn = func(int) (*exec.Cmd, error) {
		cmd := exec.Command("sleep", "30")
		return cmd, cmd.Start()
	}
	r := NewRunner(SpawnConfig{WorkspaceID: "acme", Dir: "/tmp/a"}, start, lock)
	r.Sleep = func(context.Context, time.Duration) {}
	r.HealthyAfter = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { return lock.Held() })
	cancel()
	<-done

	if lock.Held() {
		t.Error("Run() must release the wake lock before returning, or a crashed supervisor leaves the machine awake overnight")
	}
}

func TestRunnerDoesNotTakeWakeLockWhenOff(t *testing.T) {
	start, _ := scriptedStarter()
	lock := NewWakeLock(WakeLockOff)
	r := NewRunner(SpawnConfig{WorkspaceID: "acme", Dir: "/tmp/a", WakeLock: WakeLockOff}, start, lock)
	r.Sleep = func(context.Context, time.Duration) {}
	r.HealthyAfter = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { return r.State().Running })
	if lock.Held() {
		t.Error("wakeLock off must not hold a lock")
	}
	cancel()
	<-done
}

func TestRunnerReportsStartFailure(t *testing.T) {
	sentinel := errors.New("claude: no such file")
	failing := func(context.Context, SpawnConfig) (Process, error) { return nil, sentinel }

	r := testRunner(t, failing)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a repeatedly failing start must terminate rather than spin forever")
	}
	if !r.State().Disabled {
		t.Error("a binary that never launches must disable the workspace, not retry indefinitely")
	}
}

func TestStateIsSafeUnderConcurrentReads(t *testing.T) {
	start, _ := scriptedStarter()
	r := testRunner(t, start)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = r.State()
			}
		}()
	}
	wg.Wait()
	cancel()
	<-done
}

func TestWakeLockModeDefaultsToSession(t *testing.T) {
	if got := (SpawnConfig{}).WakeLockMode(); got != WakeLockSession {
		t.Errorf("default wake lock mode = %q, want %q", got, WakeLockSession)
	}
	if got := (SpawnConfig{WakeLock: WakeLockOff}).WakeLockMode(); got != WakeLockOff {
		t.Errorf("explicit mode = %q, want %q", got, WakeLockOff)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// The backoff must restart from the beginning after a healthy run. An earlier
// version zeroed the counter and then incremented it in the same pass, so the
// delay pinned at the second step forever.
func TestBackoffResetsAfterAHealthyRun(t *testing.T) {
	start, _ := scriptedStarter(
		&fakeProcess{pid: 1, code: 1, exitNow: true},                 // startup failure
		&fakeProcess{pid: 2, code: 1, exitNow: true},                 // and another
		&fakeProcess{pid: 3, code: 0, uptime: 20 * time.Millisecond}, // healthy
		&fakeProcess{pid: 4, code: 1, exitNow: true},                 // fails again
	)
	r := testRunner(t, start)

	var delays []time.Duration
	var mu sync.Mutex
	r.Sleep = func(_ context.Context, d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(delays) >= 4 })
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	first := DefaultBackoff[0]
	if delays[0] != first {
		t.Errorf("first delay = %v, want %v", delays[0], first)
	}
	if delays[1] == first {
		t.Error("the second consecutive failure should back off further")
	}
	// The fourth restart follows a healthy run, so it starts over.
	if delays[3] != first {
		t.Errorf("delay after a healthy run = %v, want the backoff reset to %v", delays[3], first)
	}
}

// Stop must keep it stopped. Without an explicit flag the loop reads the exit
// as an ordinary one and starts the process straight back up.
func TestStopKeepsItStopped(t *testing.T) {
	start, calls := scriptedStarter()
	r := testRunner(t, start)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { return r.State().Running })
	r.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() should return after Stop()")
	}
	if got := *calls; got != 1 {
		t.Errorf("started %d processes, want 1 — Stop must not be undone by a restart", got)
	}
}

// Stop during a restart backoff had nothing to signal — no process, a live
// context, an uninterrupted sleep — so the loop went on to start a session
// nothing would ever stop.
func TestStopDuringBackoffDoesNotStartAgain(t *testing.T) {
	start, calls := scriptedStarter(&fakeProcess{pid: 1, code: 1, exitNow: true})

	r := NewRunner(SpawnConfig{WorkspaceID: "acme", Dir: "/tmp/a", WakeLock: WakeLockOff}, start, NewWakeLock(WakeLockOff))
	r.HealthyAfter = time.Millisecond

	// A backoff long enough that only an interrupt can end it.
	sleeping := make(chan struct{})
	var once sync.Once
	r.Sleep = func(ctx context.Context, _ time.Duration) {
		once.Do(func() { close(sleeping) })
		<-ctx.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	<-sleeping
	r.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() during a backoff must end the loop")
	}
	if got := *calls; got != 1 {
		t.Errorf("started %d processes, want 1 — Stop must not be followed by another start", got)
	}
}

func TestSessionEndHookRunsBeforeTheReplacementStarts(t *testing.T) {
	// The state a session leaves behind is only both final and current in the
	// gap between the old process exiting and the new one starting.
	start, _ := scriptedStarter(
		&fakeProcess{pid: 1, code: 0, uptime: 20 * time.Millisecond},
	)
	r := testRunner(t, start)

	var mu sync.Mutex
	var causes []ExitCause
	var notified []string
	r.OnSessionEnd = func(d Decision) string {
		mu.Lock()
		causes = append(causes, d.Cause)
		mu.Unlock()
		return "was on feature/referral"
	}
	r.Notify = func(_, body string) {
		mu.Lock()
		notified = append(notified, body)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(notified) > 0 })
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(causes) == 0 || causes[0] != CauseNetworkTimeout {
		t.Errorf("hook saw causes %v, want it called with the network timeout", causes)
	}
	// The summary has to reach the notification: that line is the only thing
	// most people will ever read about the restart.
	if !strings.Contains(notified[0], "was on feature/referral") {
		t.Errorf("notification %q does not carry the handover summary", notified[0])
	}
}

func TestSessionEndHookIsSkippedOnARequestedStop(t *testing.T) {
	// Closing a session deliberately does not need a handover note about the
	// thing you just closed.
	start, _ := scriptedStarter(&fakeProcess{pid: 1, code: 0})
	r := testRunner(t, start)

	var mu sync.Mutex
	called := 0
	r.OnSessionEnd = func(Decision) string {
		mu.Lock()
		called++
		mu.Unlock()
		return "should not appear"
	}

	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(context.Background()) }()

	waitFor(t, func() bool { return r.State().Running })
	r.Stop()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if called != 0 {
		t.Errorf("hook ran %d times on a requested stop, want 0", called)
	}
}

func TestSessionEndHookRunsWhenAWorkspaceIsDisabled(t *testing.T) {
	// A workspace disabled by an auth failure is the case where the note is
	// worth most: nothing will restart to write one later.
	start, _ := scriptedStarter(
		&fakeProcess{pid: 1, code: 1, exitNow: true, output: "Remote Control requires a claude.ai subscription"},
	)
	r := testRunner(t, start)

	var mu sync.Mutex
	called := 0
	r.OnSessionEnd = func(Decision) string {
		mu.Lock()
		called++
		mu.Unlock()
		return ""
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v, want nil after disabling", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if called != 1 {
		t.Errorf("hook ran %d times, want 1 when the workspace is disabled", called)
	}
}

func TestEmptySessionEndSummaryLeavesTheNotificationAlone(t *testing.T) {
	start, _ := scriptedStarter(
		&fakeProcess{pid: 1, code: 0, uptime: 20 * time.Millisecond},
	)
	r := testRunner(t, start)
	r.OnSessionEnd = func(Decision) string { return "" }

	var mu sync.Mutex
	var notified []string
	r.Notify = func(_, body string) {
		mu.Lock()
		notified = append(notified, body)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(notified) > 0 })
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if strings.HasSuffix(notified[0], " · ") {
		t.Errorf("notification %q has a dangling separator with nothing after it", notified[0])
	}
}

func TestRunnerReportsSessionURLWhileRunningAndClearsItOnExit(t *testing.T) {
	proc := &fakeProcess{pid: 42, stopped: make(chan struct{})}
	start := func(_ context.Context, cfg SpawnConfig) (Process, error) {
		cfg.OnSessionURL("https://claude.ai/code/session-123")
		return proc, nil
	}
	cfg := SpawnConfig{WorkspaceID: "acme", Dir: "/tmp/acme", Origin: OriginRemote, Profile: "work", WakeLock: WakeLockOff}
	r := NewRunner(cfg, start, NewWakeLock(WakeLockOff))
	r.Sleep = func(context.Context, time.Duration) {}

	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(context.Background()) }()

	waitFor(t, func() bool {
		s := r.State()
		return s.Running && s.SessionURL == "https://claude.ai/code/session-123"
	})
	s := r.State()
	if s.Origin != OriginRemote || s.Profile != "work" {
		t.Errorf("origin/profile = %q/%q, want remote/work", s.Origin, s.Profile)
	}

	r.Stop()
	<-done
	if got := r.State().SessionURL; got != "" {
		t.Errorf("sessionUrl = %q after exit; a new session gets a new URL, so it must clear", got)
	}
}

func TestSupervisingReportsStopAndDisable(t *testing.T) {
	start, _ := scriptedStarter()
	r := testRunner(t, start)
	if !r.Supervising() {
		t.Fatal("a fresh runner must report itself supervising")
	}
	r.Stop()
	if r.Supervising() {
		t.Error("a stopped runner must not report itself supervising — a restart needs a fresh runner")
	}
}
