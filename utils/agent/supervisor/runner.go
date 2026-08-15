package supervisor

import (
	"context"
	"os"
	"sync"
	"time"
)

// Process is a running remote-control instance. Abstracted so the supervisor's
// restart logic can be tested without a claude binary, a subscription, or a
// network.
type Process interface {
	// Pid is the process id, used to tie the wake lock to its lifetime.
	Pid() int
	// Wait blocks until the process exits, returning its code and a tail of
	// its combined output for exit classification.
	Wait() (code int, output string)
	// Stop asks the process to terminate.
	Stop()
}

// Starter launches one remote-control process.
type Starter func(ctx context.Context, cfg SpawnConfig) (Process, error)

// RunState is the supervisor's view of one workspace, as reported by
// `corgi agent status`.
type RunState struct {
	WorkspaceID string    `json:"workspaceId"`
	Running     bool      `json:"running"`
	PID         int       `json:"pid,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	Restarts    int       `json:"restarts"`
	Disabled    bool      `json:"disabled,omitempty"`
	LastCause   ExitCause `json:"lastCause,omitempty"`
	LastReason  string    `json:"lastReason,omitempty"`
	WakeLock    bool      `json:"wakeLock"`
}

// Runner supervises one workspace's remote-control process.
type Runner struct {
	Config   SpawnConfig
	Start    Starter
	WakeLock *WakeLock
	// Notify reports a restart or a shutdown to the user. Optional.
	Notify func(title, body string)
	// OnSessionEnd runs after a supervised process exits and before its
	// replacement starts, while whatever the session left on disk is still
	// there. It returns a line to append to the restart notification, or "" when
	// there is nothing worth adding. Optional.
	//
	// This is the only moment the state is both final and current, which is why
	// it is a hook here rather than something the daemon polls for.
	OnSessionEnd func(Decision) string
	// Sleep is the delay between restarts. Injected so tests do not wait.
	Sleep func(ctx context.Context, d time.Duration)
	// HealthyAfter is how long a run must last to count as healthy, resetting
	// the failure streak. Zero means MinHealthyUptime.
	HealthyAfter time.Duration
	// OnChange fires after the run state changes, so a watcher can republish
	// without polling. Called without the lock held.
	OnChange func()

	mu       sync.Mutex
	state    RunState
	proc     Process
	stopping bool
	// stopped closes when Stop is called, so a backoff sleep can be cut short
	// rather than running to completion and starting the process again.
	stopped     chan struct{}
	stoppedOnce sync.Once
}

// NewRunner returns a Runner with the real sleep behaviour.
func NewRunner(cfg SpawnConfig, start Starter, lock *WakeLock) *Runner {
	return &Runner{
		Config:   cfg,
		Start:    start,
		WakeLock: lock,
		Sleep:    sleepWithContext,
		state:    RunState{WorkspaceID: cfg.WorkspaceID},
		stopped:  make(chan struct{}),
	}
}

// State returns a snapshot for status output.
func (r *Runner) State() RunState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.WakeLock = r.WakeLock.Held()
	return s
}

// Stop asks the supervised process to exit and keeps it stopped.
//
// The flag matters: without it the loop sees an ordinary exit, classifies it as
// a crash or a network timeout, and starts the process straight back up.
func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopping = true
	proc := r.proc
	r.mu.Unlock()

	// Wake a backoff sleep. Without this, Stop during the gap between restarts
	// did nothing at all — no process to signal, the context still live — and
	// the loop went on to start a session nothing would ever stop.
	r.stoppedOnce.Do(func() {
		if r.stopped != nil {
			close(r.stopped)
		}
	})
	if proc != nil {
		proc.Stop()
	}
}

// stopRequested reports whether Stop has been called.
func (r *Runner) stopRequested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

// Run supervises until ctx is cancelled or the workspace is disabled.
//
// It always returns with the wake lock released and no process left running,
// so a caller can rely on Run's return meaning "nothing of mine is still up".
func (r *Runner) Run(ctx context.Context) error {
	defer r.WakeLock.Release()

	// `always` holds the lock for the whole supervised lifetime, including the
	// gaps between restarts. Releasing it per-process would make the mode
	// identical to `session`, and the machine could sleep during a five-minute
	// backoff and never come back.
	alwaysAwake := r.Config.WakeLockMode() == WakeLockAlways
	if alwaysAwake {
		_ = r.WakeLock.Acquire(os.Getpid())
	}

	attempt := 0
	startupFailures := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.stopRequested() {
			return nil
		}

		exit, startErr := r.runOnce(ctx, alwaysAwake)
		decision := Decide(exit, attempt, startupFailures)
		healthy := decision.Cause != CauseStartupFailure

		if healthy {
			// A run that lasted long enough to be useful clears the streak, so
			// one bad night does not disable a workspace weeks later.
			startupFailures = 0
		} else {
			startupFailures++
		}

		r.record(decision, 0, decision.Disable)
		r.announce(decision, r.captureSessionEnd(decision))

		if !decision.Restart {
			return stopReason(decision, startErr, ctx)
		}

		// Reset AFTER choosing this delay, so the next failure starts from the
		// beginning of the backoff. Zeroing before the increment left it pinned
		// at the second step forever.
		if healthy {
			attempt = 0
		} else {
			attempt++
		}
		r.sleepUnlessStopped(ctx, decision.Delay)
	}
}

// sleepUnlessStopped waits out the backoff, returning early if Stop is called.
func (r *Runner) sleepUnlessStopped(ctx context.Context, d time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Sleep(ctx, d)
	}()
	select {
	case <-done:
	case <-r.stopped:
	case <-ctx.Done():
	}
}

// runOnce starts the process and waits for it, returning the exit to classify.
// A launch that never got off the ground reports as an instant failure, so it
// falls under the same give-up rule as one that exits immediately.
func (r *Runner) runOnce(ctx context.Context, alwaysAwake bool) (Exit, error) {
	proc, err := r.Start(ctx, r.Config)
	if err != nil {
		return Exit{Code: -1, Output: err.Error(), healthyAfter: r.healthyAfter()}, err
	}

	startedAt := time.Now()
	r.markRunning(proc, startedAt)

	if r.Config.WakeLockMode() == WakeLockSession {
		// A failure here is not fatal: the session is more useful awake-only
		// than not running at all. Surfaced through status instead.
		_ = r.WakeLock.Acquire(proc.Pid())
	}

	code, output := proc.Wait()
	uptime := time.Since(startedAt)
	if !alwaysAwake {
		r.WakeLock.Release()
	}

	return Exit{
		Code:         code,
		Uptime:       uptime,
		Output:       output,
		Requested:    ctx.Err() != nil || r.stopRequested(),
		healthyAfter: r.healthyAfter(),
	}, nil
}

// stopReason is what Run returns when it will not restart: the launch error if
// the process never started, nil once a workspace is deliberately disabled, and
// otherwise whatever ended the context.
func stopReason(d Decision, startErr error, ctx context.Context) error {
	if startErr != nil && !d.Disable {
		return startErr
	}
	if d.Disable {
		return nil
	}
	return ctx.Err()
}

func (r *Runner) markRunning(proc Process, startedAt time.Time) {
	r.mu.Lock()
	r.proc = proc
	r.state.Running = true
	r.state.PID = proc.Pid()
	r.state.StartedAt = startedAt
	r.mu.Unlock()
	r.notifyChange()
}

// notifyChange runs the watcher callback with no lock held: it will read State,
// which takes the same lock.
func (r *Runner) notifyChange() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

func (r *Runner) record(d Decision, pid int, disabled bool) {
	r.recordLocked(d, pid, disabled)
	r.notifyChange()
}

func (r *Runner) recordLocked(d Decision, pid int, disabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proc = nil
	r.state.Running = false
	r.state.PID = pid
	r.state.LastCause = d.Cause
	r.state.LastReason = d.Reason
	if disabled {
		r.state.Disabled = true
	}
	if d.Restart {
		r.state.Restarts++
	}
}

// captureSessionEnd records what the ending session left behind.
//
// Only for an end that actually replaces or stops the session: a requested stop
// is the user closing it deliberately, and they do not need a handover note for
// something they just did.
func (r *Runner) captureSessionEnd(d Decision) string {
	if r.OnSessionEnd == nil || !(d.Restart || d.Disable) {
		return ""
	}
	return r.OnSessionEnd(d)
}

func (r *Runner) announce(d Decision, detail string) {
	if !d.Notify || r.Notify == nil {
		return
	}
	body := d.Reason
	if detail != "" {
		body += " · " + detail
	}
	r.Notify("corgi agent · "+r.Config.WorkspaceID, body)
}

// healthyAfter is how long a run must last before the failure streak resets.
func (r *Runner) healthyAfter() time.Duration {
	if r.HealthyAfter > 0 {
		return r.HealthyAfter
	}
	return MinHealthyUptime
}

// WakeLockMode returns the configured mode, defaulting to session scope.
func (c SpawnConfig) WakeLockMode() WakeLockMode {
	if c.WakeLock == "" {
		return WakeLockSession
	}
	return c.WakeLock
}

// sleepWithContext waits for d, or returns early when ctx is cancelled, so a
// shutdown during a five-minute backoff is not held up by it.
func sleepWithContext(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
