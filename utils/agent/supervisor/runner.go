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
	// Sleep is the delay between restarts. Injected so tests do not wait.
	Sleep func(ctx context.Context, d time.Duration)
	// HealthyAfter is how long a run must last to count as healthy, resetting
	// the failure streak. Zero means MinHealthyUptime.
	HealthyAfter time.Duration
	// OnChange fires after the run state changes, so a watcher can republish
	// without polling. Called without the lock held.
	OnChange func()

	mu    sync.Mutex
	state RunState
	proc  Process
}

// NewRunner returns a Runner with the real sleep behaviour.
func NewRunner(cfg SpawnConfig, start Starter, lock *WakeLock) *Runner {
	return &Runner{
		Config:   cfg,
		Start:    start,
		WakeLock: lock,
		Sleep:    sleepWithContext,
		state:    RunState{WorkspaceID: cfg.WorkspaceID},
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

// Stop asks the supervised process to exit. The Run loop sees the cancelled
// context and treats the exit as requested rather than restarting it.
func (r *Runner) Stop() {
	r.mu.Lock()
	proc := r.proc
	r.mu.Unlock()
	if proc != nil {
		proc.Stop()
	}
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

		proc, err := r.Start(ctx, r.Config)
		if err != nil {
			// A launch that never got off the ground is a startup failure, and
			// is subject to the same give-up rule as one that exits instantly.
			decision := Decide(Exit{Code: -1, Output: err.Error()}, attempt, startupFailures)
			startupFailures++
			r.record(decision, 0, decision.Disable)
			r.announce(decision)
			if !decision.Restart {
				return err
			}
			attempt++
			r.Sleep(ctx, decision.Delay)
			continue
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

		exit := Exit{
			Code:         code,
			Uptime:       uptime,
			Output:       output,
			Requested:    ctx.Err() != nil,
			healthyAfter: r.healthyAfter(),
		}

		decision := Decide(exit, attempt, startupFailures)
		if decision.Cause == CauseStartupFailure {
			startupFailures++
		} else {
			// A run that lasted long enough to be useful clears the streak, so
			// one bad night does not disable a workspace weeks later.
			startupFailures = 0
			attempt = 0
		}

		r.record(decision, 0, decision.Disable)
		r.announce(decision)

		if !decision.Restart {
			if decision.Disable {
				return nil
			}
			return ctx.Err()
		}

		attempt++
		r.Sleep(ctx, decision.Delay)
	}
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

func (r *Runner) announce(d Decision) {
	if !d.Notify || r.Notify == nil {
		return
	}
	r.Notify("corgi agent · "+r.Config.WorkspaceID, d.Reason)
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
