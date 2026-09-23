package supervisor

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Process interface {
	Pid() int
	Wait() (code int, output string)
	Stop()
}

type Starter func(ctx context.Context, cfg SpawnConfig) (Process, error)

type RunState struct {
	WorkspaceID     string    `json:"workspaceId"`
	Running         bool      `json:"running"`
	PID             int       `json:"pid,omitempty"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	Restarts        int       `json:"restarts"`
	Disabled        bool      `json:"disabled,omitempty"`
	LastCause       ExitCause `json:"lastCause,omitempty"`
	LastReason      string    `json:"lastReason,omitempty"`
	WakeLock        bool      `json:"wakeLock"`
	Origin          string    `json:"origin,omitempty"`
	Profile         string    `json:"profile,omitempty"`
	SessionURL      string    `json:"sessionUrl,omitempty"`
	Sessions        []string  `json:"sessions,omitempty"`
	DeviceOnly      bool      `json:"deviceOnly,omitempty"`
	SessionsThisRun int       `json:"sessionsThisRun,omitempty"`
	Note            string    `json:"note,omitempty"`
}

const maxTrackedSessions = 20

type RunEvent struct {
	Kind   string
	PID    int
	Cause  string
	Reason string
	URL    string
}

type Runner struct {
	Config       SpawnConfig
	Start        Starter
	WakeLock     *WakeLock
	Notify       func(title, body string)
	OnSessionEnd func(Decision) string
	Sleep        func(ctx context.Context, d time.Duration)
	HealthyAfter time.Duration
	OnChange     func()
	OnEvent      func(RunEvent)
	IdleAfter    time.Duration

	lastActivity atomic.Int64

	mu          sync.Mutex
	state       RunState
	proc        Process
	stopping    bool
	stopped     chan struct{}
	stoppedOnce sync.Once
}

func NewRunner(cfg SpawnConfig, start Starter, lock *WakeLock) *Runner {
	r := &Runner{
		Config:   cfg,
		Start:    start,
		WakeLock: lock,
		Sleep:    sleepWithContext,
		state:    RunState{WorkspaceID: cfg.WorkspaceID, Origin: cfg.Origin, Profile: cfg.Profile},
		stopped:  make(chan struct{}),
	}
	r.Config.OnSessionURL = r.setSessionURL
	r.Config.OnSessionLink = r.addSessionLink
	r.Config.OnActivity = r.recordActivity
	return r
}

func (r *Runner) addSessionLink(id string) {
	url := "https://claude.ai/code/" + id
	r.mu.Lock()
	r.state.SessionsThisRun++
	for _, s := range r.state.Sessions {
		if s == url {
			r.mu.Unlock()
			return
		}
	}
	r.state.Sessions = append(r.state.Sessions, url)
	if len(r.state.Sessions) > maxTrackedSessions {
		r.state.Sessions = r.state.Sessions[len(r.state.Sessions)-maxTrackedSessions:]
	}
	r.mu.Unlock()
	r.emit(RunEvent{Kind: "session", URL: url})
	r.notifyChange()
}

func (r *Runner) emit(e RunEvent) {
	if r.OnEvent != nil {
		r.OnEvent(e)
	}
}

func (r *Runner) recordActivity() {
	r.lastActivity.Store(time.Now().UnixNano())
}

func (r *Runner) idleAfter() time.Duration {
	if r.IdleAfter > 0 {
		return r.IdleAfter
	}
	return WakeLockIdleTimeout
}

func (r *Runner) startIdleMonitor(ctx context.Context, pid int) func() {
	idleAfter := r.idleAfter()
	tick := idleAfter / 4
	if tick < 20*time.Millisecond {
		tick = 20 * time.Millisecond
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				last := r.lastActivity.Load()
				if last != 0 && time.Since(time.Unix(0, last)) >= idleAfter {
					r.WakeLock.Release()
				} else {
					_ = r.WakeLock.Acquire(pid)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

func (r *Runner) setSessionURL(url string) {
	r.mu.Lock()
	if r.state.SessionURL == url {
		r.mu.Unlock()
		return
	}
	r.state.SessionURL = url
	r.mu.Unlock()
	r.notifyChange()
}

func (r *Runner) Supervising() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.stopping && !r.state.Disabled
}

func (r *Runner) State() RunState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.WakeLock = r.WakeLock.Held()
	return s
}

func (r *Runner) Stop() {
	proc := r.beginStop()
	if proc != nil {
		proc.Stop()
	}
}

func (r *Runner) StopAsync() {
	proc := r.beginStop()
	if proc != nil {
		go proc.Stop()
	}
}

func (r *Runner) beginStop() Process {
	r.mu.Lock()
	r.stopping = true
	proc := r.proc
	r.mu.Unlock()

	r.stoppedOnce.Do(func() {
		if r.stopped != nil {
			close(r.stopped)
		}
	})
	return proc
}

func (r *Runner) stopRequested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

func (r *Runner) Run(ctx context.Context) error {
	defer r.WakeLock.Release()

	alwaysAwake := r.Config.WakeLockMode() == WakeLockAlways
	if alwaysAwake {
		_ = r.WakeLock.Acquire(os.Getpid())
	}

	var s streak
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.stopRequested() {
			return nil
		}

		exit, startErr := r.runOnce(ctx, alwaysAwake)
		if r.retryWithoutUnsupportedFlag(exit) {
			continue
		}
		decision := Decide(exit, s.attempt, s.startupFailures)
		healthy := decision.Cause != CauseStartupFailure
		s.observe(healthy)

		r.record(decision, 0, decision.Disable)
		r.announce(decision, r.captureSessionEnd(decision))

		if !decision.Restart {
			return stopReason(decision, startErr, ctx)
		}
		s.advance(healthy)
		r.sleepUnlessStopped(ctx, decision.Delay)
	}
}

type streak struct {
	attempt         int
	startupFailures int
}

func (s *streak) observe(healthy bool) {
	if healthy {
		s.startupFailures = 0
		return
	}
	s.startupFailures++
}

func (s *streak) advance(healthy bool) {
	if healthy {
		s.attempt = 0
		return
	}
	s.attempt++
}

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

func (r *Runner) runOnce(ctx context.Context, alwaysAwake bool) (Exit, error) {
	r.mu.Lock()
	r.state.SessionsThisRun = 0
	r.state.DeviceOnly = r.Config.DeviceOnly
	r.mu.Unlock()
	proc, err := r.Start(ctx, r.Config)
	if err != nil {
		return Exit{Code: -1, Output: err.Error(), healthyAfter: r.healthyAfter()}, err
	}

	startedAt := time.Now()
	r.markRunning(proc, startedAt)

	if r.stopRequested() {
		proc.Stop()
	}

	var stopIdle func()
	switch r.Config.WakeLockMode() {
	case WakeLockSession:
		_ = r.WakeLock.Acquire(proc.Pid())
	case WakeLockIdle:
		if Supported() {
			r.recordActivity()
			_ = r.WakeLock.Acquire(proc.Pid())
			stopIdle = r.startIdleMonitor(ctx, proc.Pid())
		}
	}

	code, output := proc.Wait()
	uptime := time.Since(startedAt)
	if stopIdle != nil {
		stopIdle()
	}
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

const unknownOptionMarker = "unknown option"

const unsupportedFlagNote = "this Claude Code predates " + DeviceOnlyFlag +
	" - a session is opened in the checkout at every start; update Claude Code to stop that"

func flagUnsupported(output, flag string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, unknownOptionMarker) && strings.Contains(lower, strings.ToLower(flag))
}

func (r *Runner) retryWithoutUnsupportedFlag(e Exit) bool {
	if e.Requested || e.Uptime >= e.healthyThreshold() {
		return false
	}
	r.mu.Lock()
	if !r.Config.DeviceOnly || !flagUnsupported(e.Output, DeviceOnlyFlag) {
		r.mu.Unlock()
		return false
	}
	r.Config.DeviceOnly = false
	r.proc = nil
	r.state.Running = false
	r.state.Note = unsupportedFlagNote
	r.mu.Unlock()
	r.emit(RunEvent{Kind: "exited", Cause: string(CauseUnsupportedFlag), Reason: unsupportedFlagNote})
	r.notifyChange()
	return true
}

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
	r.emit(RunEvent{Kind: "started", PID: proc.Pid()})
	r.notifyChange()
}

func (r *Runner) notifyChange() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

func (r *Runner) record(d Decision, pid int, disabled bool) {
	r.recordLocked(d, pid, disabled)
	kind := "exited"
	if disabled {
		kind = "disabled"
	}
	r.emit(RunEvent{Kind: kind, Cause: string(d.Cause), Reason: d.Reason})
	r.notifyChange()
}

func (r *Runner) recordLocked(d Decision, pid int, disabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proc = nil
	r.state.Running = false
	r.state.SessionURL = ""
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

func (r *Runner) healthyAfter() time.Duration {
	if r.HealthyAfter > 0 {
		return r.HealthyAfter
	}
	return MinHealthyUptime
}

func (c SpawnConfig) WakeLockMode() WakeLockMode {
	if c.WakeLock == "" {
		return WakeLockSession
	}
	return c.WakeLock
}

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
