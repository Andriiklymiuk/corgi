// Package daemon runs one supervisor per workspace and reports what they are
// doing. It owns no session state of its own: Remote Control owns sessions,
// corgi owns keeping Remote Control up.
package daemon

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// Info is the daemon's own record, written so `corgi agent status` and
// `corgi agent stop` can find a running daemon from another process.
type Info struct {
	PID     int    `json:"pid"`
	Version string `json:"version"`
	// Executable is recorded so a stale record cannot make `corgi agent stop`
	// signal an unrelated process that happened to inherit the pid.
	Executable string    `json:"executable,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	Workspaces []string  `json:"workspaces"`
	// Commands is true when this daemon drains the command spool and handles a
	// SIGUSR1 nudge. A daemon from before this feature omits it, and Nudge must
	// then NOT signal it — SIGUSR1's default disposition would kill it.
	Commands bool `json:"commands,omitempty"`
}

// Status is the whole daemon's state, as `corgi agent status --json` prints.
type Status struct {
	Running      bool                  `json:"running"`
	PID          int                   `json:"pid,omitempty"`
	StartedAt    time.Time             `json:"startedAt,omitempty"`
	Version      string                `json:"version,omitempty"`
	WakeLockable bool                  `json:"wakeLockSupported"`
	Workspaces   []supervisor.RunState `json:"workspaces"`
	Diagnostics  []WorkspaceDiagnostic `json:"diagnostics,omitempty"`
}

// WorkspaceDiagnostic is the per-workspace startup line that prevents the most
// likely surprise in agent mode: work silently running under the wrong Claude
// account. Printed at start and by `corgi agent status`.
type WorkspaceDiagnostic struct {
	WorkspaceID string   `json:"workspaceId"`
	Dir         string   `json:"dir"`
	Kind        string   `json:"kind,omitempty"`
	Bin         string   `json:"bin"`
	ConfigDir   string   `json:"configDir"`
	Spawn       string   `json:"spawn"`
	Stripped    []string `json:"strippedCredentials,omitempty"`
	Warning     string   `json:"warning,omitempty"`
}

// Daemon supervises every autostart workspace.
type Daemon struct {
	// recentAttention remembers what was just sent, so a duplicate stays quiet.
	attentionMu     sync.Mutex
	recentAttention map[string]time.Time
	// limitWatch: sessions that left "limited"; the notice waits for a finished turn.
	limitWatch map[string]bool

	// Watches are the workspaces that poll their tracker and code host.
	Watches    []WatchSpec
	watchState *watch.State
	watchers   map[string]*watch.Watch
	fixBusy    map[string]*sync.Mutex
	fixActive  map[string]bool

	Version string
	// Dir is the agent data directory holding daemon.json and registry.json.
	Dir string
	// Start launches a remote-control process; injected for tests.
	Start supervisor.Starter
	// Notify reports restarts. Defaults to corgi's desktop notification.
	Notify         func(title, body string)
	NotifyWithLink func(title, body, link string)
	LinkFor        func(workspaceID string) string
	// Pickup moves a ticket to its workspace's pickup column when an
	// unattended fix takes it on, so the board shows it is being worked on.
	// Injected because writing to a tracker belongs to the command layer.
	Pickup func(workspace string, e watch.Event)
	// ClaimTicket takes the ticket on the tracker for this machine, or names
	// the machine that already has it. Injected for the same reason.
	ClaimTicket func(workspace string, e watch.Event) (ok bool, holder string, err error)
	// Delivered moves a ticket on once a run has opened a pull request for
	// it: the work is done and it is waiting on a reviewer, which is a
	// different column from the one it was picked up into.
	Delivered func(workspace string, e watch.Event, prs []string)
	// Workpad writes one section of the ticket's corgi comment: the handoff
	// a run left, the reason it is blocked. Nil when the workspace has no
	// tracker token.
	Workpad func(workspace, ref, section, text string)
	// Push reaches the paired phones: every attention notification, and a
	// permission prompt with the session id so the phone can answer it from
	// the lock screen. Nil when nothing is paired.
	Push func(m push.Message)
	// Isolate gives a workspace's repositories worktrees on a branch and
	// returns their directories, for a watch with isolate on. Nil means
	// runs happen in the checkout.
	Isolate func(dir, branch string) ([]string, error)
	Events  *events.Log
	// CaptureBrief probes what an ending session left on disk. Injected because
	// enumerating a stack's repositories means parsing a compose file, which the
	// daemon has no business knowing about. Nil disables briefs entirely.
	CaptureBrief func(brief.Params) *brief.Brief
	// ResolveWorkspace builds launch settings for one workspace on demand —
	// remote session start. Injected by cmd, which knows the registry and
	// config files; nil disables commands and keeps the fixed-set lifecycle
	// exactly as it was.
	ResolveWorkspace func(workspaceID, profile, name string) (supervisor.SpawnConfig, error)
	// CommandTick is the spool poll interval; the SIGUSR1 nudge only shortens
	// the wait. Zero means statusPublishInterval. Test seam.
	CommandTick time.Duration
	// IdleTick replaces every poll interval while nothing is tracked or
	// supervised. Zero means idleInterval. Test seam.
	IdleTick time.Duration

	// Sessions is the registry of interactive Claude Code sessions, fed by
	// hooks and published as sessions.json. Nil turns tracking off.
	Sessions *sessions.Registry
	// MergePull merges a pull request of mine at the forge (the workspace's
	// autoMerge); nil means the daemon never merges.
	MergePull func(ctx context.Context, workspace, link string) error
	// Raise brings a session's window to the front; nil means the platform
	// default. Alive, ListProcesses and Cwd are the process probes the
	// reaper and rescan use. All test seams.
	Raise         func(ctx context.Context, t sessions.FocusTarget) error
	Alive         func(pid int) bool
	ListProcesses func() ([]proc.Process, error)
	Cwd           func(pid int) string
	// ReapTick overrides reapInterval.
	ReapTick time.Duration
	// AccountDirs lists the config dirs of every configured profile, so the
	// board shows an account before a session runs under it. Injected by
	// cmd. TypeText is the emulator typing seam.
	AccountDirs func() []string
	TypeText    func(ctx context.Context, t sessions.FocusTarget, text string, enter bool) error
	// AutoContinue types "continue" into limited sessions when their limit
	// should be over; see autocontinue.go.
	AutoContinue bool
	// SessionCap is the token budget every session runs under unless it
	// has one of its own; zero means none. Read on the sweep, so a change
	// by command takes at once. Spend is read from each transcript from
	// where the last sweep stopped; see spend.go.
	SessionCap int64
	spent      map[string]spendMark
	// DigestAt is the local HH:MM for the daily digest; Digest builds its
	// text. Both injected by cmd; either empty means no digest.
	DigestAt string
	Digest   func(now time.Time) string
	// publishStopped is called as the status publisher exits.
	//
	// A test seam. Whether Run waits for that goroutine is otherwise observable
	// only as a race — the publisher writing into a directory Run has finished
	// with — which a test can lose a hundred times before catching once.
	publishStopped func()

	mu        sync.Mutex
	runners   []*supervisor.Runner
	diags     []WorkspaceDiagnostic
	startedAt time.Time
	// autostart is the startup set by workspace id. A workspace that came up
	// with the daemon goes back to that after a remote stop, so "Stop" from
	// the phone ends the session without also taking the machine offline.
	autostart map[string]supervisor.SpawnConfig
	// replacing marks workspaces whose process is being swapped for another —
	// a device-only server being given a session, or a stopped session going
	// back to device-only. A second start in that window would race the swap.
	replacing map[string]bool
	// swaps counts relaunch goroutines in flight. Run drains it before it
	// waits for the runners: a swap that launches after the runner wait has
	// begun would add to a WaitGroup already being waited on.
	swaps sync.WaitGroup
	// runs counts unattended fixes in flight, so a test or a shutdown can
	// wait for the last one to write its record.
	runs sync.WaitGroup
	// routines remembers when each scheduled run last started.
	routines *routineState

	// nudge wakes the command loop; the cross-process doorbell is SIGUSR1.
	nudge chan struct{}

	// publishSignal is nudged whenever a supervisor's state changes, so
	// `corgi agent status` in another process is not up to five seconds stale.
	publishSignal chan struct{}
	// reapSignal re-arms the reaper's ticker when the daemon goes idle or busy.
	reapSignal chan struct{}
}

// New returns a Daemon writing state under dir.
func New(version, dir string) *Daemon {
	return &Daemon{
		Version:        version,
		Dir:            dir,
		Start:          supervisor.StartProcess,
		Notify:         utils.Notify,
		NotifyWithLink: utils.NotifyWithLink,
		Events:         events.NewLog(dir),
		Sessions:       sessions.New(SessionsPath(dir), sessions.DefaultSize),
		ListProcesses:  proc.List,
		Cwd:            proc.Cwd,
		publishSignal:  make(chan struct{}, 1),
		reapSignal:     make(chan struct{}, 1),
		nudge:          make(chan struct{}, 1),
	}
}

func (d *Daemon) recordEvent(workspaceID string) func(supervisor.RunEvent) {
	return func(e supervisor.RunEvent) {
		d.Events.Append(workspaceID, events.Event{
			Kind: e.Kind, PID: e.PID, Cause: e.Cause, Reason: e.Reason, URL: e.URL,
		})
		if e.Cause == string(supervisor.CauseUnsupportedFlag) {
			// The runner already dropped the flag for itself. Drop it from the
			// startup settings too, or the next swap back to a device would
			// send it again, fail again, and open the session it was meant to
			// avoid — once per phone Stop.
			d.forgetDeviceOnly(workspaceID)
		}
	}
}

func (d *Daemon) forgetDeviceOnly(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cfg, ok := d.autostart[id]; ok {
		cfg.DeviceOnly = false
		d.autostart[id] = cfg
	}
}

// Nudge wakes the command loop in this process. The cross-process variant is
// the package-level Nudge.
func (d *Daemon) Nudge() {
	select {
	case d.nudge <- struct{}{}:
	default:
	}
}

// Nudge pokes a running daemon so it drains the spool now rather than on the
// next tick. Best-effort. Signals ONLY a daemon that advertised command
// support: an older one has no SIGUSR1 handler and would be killed by the
// nudge, and cannot drain the spool anyway — tell the user to restart it.
func Nudge(info *Info) {
	if info == nil || info.PID <= 0 || !info.Commands {
		return
	}
	_ = nudgeProcess(info.PID)
}

// InfoPath is where the daemon record lives.
func (d *Daemon) InfoPath() string { return filepath.Join(d.Dir, "daemon.json") }

// StatusPath is where the daemon publishes its state for other processes.
func (d *Daemon) StatusPath() string { return filepath.Join(d.Dir, "status.json") }

// The daemon talks to other corgi processes through files — no socket, no
// port, nothing to secure — so it polls. Variables so tests can shrink them.
var (
	// statusPublishInterval is how often the running daemon republishes its
	// state and drains the spool while something is tracked or supervised.
	statusPublishInterval = 5 * time.Second
	// idleInterval is the cadence of every poll while nothing is; a nudge or
	// a new session event brings the fast one back at once.
	idleInterval = time.Minute
	// digestCheckInterval bounds how often the digest marker is read.
	digestCheckInterval = time.Minute
)

// requestPublish nudges the publisher. Non-blocking, and coalescing: a burst
// of state changes produces one write, not one per change.
func (d *Daemon) requestPublish() {
	if d.publishSignal == nil {
		return
	}
	select {
	case d.publishSignal <- struct{}{}:
	default:
	}
}

// idle is true when the daemon has nothing to watch: no session on the board
// (a gone one on a pinned key does not count) and no supervised process up.
func (d *Daemon) idle() bool {
	if d.Sessions != nil {
		for _, s := range d.Sessions.Sessions() {
			if s.Status != sessions.StatusGone {
				return false
			}
		}
	}
	for _, r := range d.Runners() {
		if r.Supervising() && r.State().Running {
			return false
		}
	}
	return true
}

// pollInterval picks the cadence for the publish, drain and reap loops: fast
// while busy, idleInterval otherwise.
func (d *Daemon) pollInterval(fast time.Duration) time.Duration {
	if fast == 0 {
		fast = statusPublishInterval
	}
	if !d.idle() {
		return fast
	}
	if d.IdleTick != 0 {
		return d.IdleTick
	}
	return idleInterval
}

// wakeLoops re-arms the publisher and the reaper so an idle-to-busy change
// takes effect now rather than at the end of a long tick.
func (d *Daemon) wakeLoops() {
	d.requestPublish()
	select {
	case d.reapSignal <- struct{}{}:
	default:
	}
}

// publishStatus keeps status.json fresh until ctx ends. It republishes on every
// state change, with a slow tick as a safety net for anything that changes
// without notifying (the wake lock, for instance). Unchanged state is not
// rewritten, so an idle daemon leaves the disk alone.
func (d *Daemon) publishStatus(ctx context.Context) {
	if d.publishStopped != nil {
		defer d.publishStopped()
	}
	ticker := time.NewTicker(d.pollInterval(0))
	defer ticker.Stop()
	var last []byte
	var digestChecked time.Time
	for {
		if data, err := json.MarshalIndent(d.Status(), "", "  "); err == nil && !bytes.Equal(data, last) {
			if writeAtomic(d.StatusPath(), data) == nil {
				last = data
			}
		}
		if now := time.Now(); now.Sub(digestChecked) >= digestCheckInterval {
			digestChecked = now
			d.sendDigestIfDue(now)
		}
		ticker.Reset(d.pollInterval(0))
		select {
		case <-ctx.Done():
			return
		case <-d.publishSignal:
		case <-ticker.C:
		}
	}
}

// ReadStatus returns the running daemon's published state, or nil when no
// daemon is running.
func ReadStatus(dir string) (*Status, error) {
	info, err := ReadInfo(dir)
	if err != nil || info == nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if os.IsNotExist(err) {
		// Running but has not published yet.
		return &Status{Running: true, PID: info.PID, StartedAt: info.StartedAt, Version: info.Version}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.StartedAt = info.StartedAt
	return &s, nil
}

// Run supervises every config until ctx is cancelled. One goroutine per
// workspace, so one disabling itself does not take the others down. Returns
// only once every supervisor has finished — "nothing of mine is still up".
//
// With ResolveWorkspace set it also drains the command spool and can start and
// stop workspaces at runtime.
func (d *Daemon) Run(ctx context.Context, configs []supervisor.SpawnConfig) error {
	// Catch SIGUSR1 for the daemon's ENTIRE lifetime — before writeInfo makes
	// this pid nudgeable, until after cleanup. Go terminates on an unhandled
	// SIGUSR1, so a nudge racing a launchd restart would kill the daemon.
	// Installed on the fixed path too, where nothing reads it, for that reason.
	stopSignals := notifyNudge(d.nudge)
	defer stopSignals()

	if d.ResolveWorkspace != nil {
		return d.runDynamic(ctx, configs)
	}
	return d.runFixed(ctx, configs)
}

// runDynamic is the command-capable lifecycle: the startup set may be empty,
// and spool commands add or stop runners while it holds the process open.
func (d *Daemon) runDynamic(ctx context.Context, configs []supervisor.SpawnConfig) error {
	for _, cfg := range configs {
		if err := supervisor.ValidateSpawnConfig(cfg); err != nil {
			return err
		}
	}
	d.startedAt = time.Now().UTC()
	d.rememberAutostart(configs)
	d.buildRunners(configs)
	if err := d.writeInfoIDs(d.runnerIDs()); err != nil {
		return err
	}
	defer d.cleanup()

	publishCtx, stopPublishing := context.WithCancel(ctx)
	defer stopPublishing()
	publishDone := make(chan struct{})
	go func() { defer close(publishDone); d.publishStatus(publishCtx) }()
	defer func() { stopPublishing(); <-publishDone }()

	var wg sync.WaitGroup
	launch := func(r *supervisor.Runner) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Run(ctx); err != nil && ctx.Err() == nil {
				utils.Infof("agent: %s stopped: %v\n", r.Config.WorkspaceID, err)
			}
		}()
	}
	for _, r := range d.Runners() {
		launch(r)
	}
	if len(configs) == 0 {
		utils.Info("agent: no autostart workspaces — waiting for remote session starts")
	}

	d.startSessionTracking()
	d.startWatches(ctx)
	reapDone := make(chan struct{})
	go func() { defer close(reapDone); d.reapSessions(ctx) }()
	defer func() { <-reapDone }()

	ticker := time.NewTicker(d.pollInterval(d.CommandTick))
	defer ticker.Stop()

	idle := d.idle()
	for ctx.Err() == nil {
		d.drainCommands(ctx, launch)
		if now := d.idle(); now != idle {
			idle = now
			d.wakeLoops()
		}
		ticker.Reset(d.pollInterval(d.CommandTick))
		select {
		case <-ctx.Done():
		case <-d.nudge:
		case <-ticker.C:
		}
	}
	// Swaps first: one still running sees the cancelled context and launches
	// nothing, but it must be finished before the runner wait begins.
	d.swaps.Wait()
	wg.Wait()
	return ctx.Err()
}

// runFixed is the pre-command lifecycle, kept byte-for-byte for callers that
// never injected ResolveWorkspace.
func (d *Daemon) runFixed(ctx context.Context, configs []supervisor.SpawnConfig) error {
	if len(configs) == 0 {
		return fmt.Errorf("no workspaces configured for agent mode — run `corgi agent init` in a stack, or `corgi agent scan <dir>`")
	}

	for _, cfg := range configs {
		if err := supervisor.ValidateSpawnConfig(cfg); err != nil {
			// Fail before launching anything: a bad setting should be a clear
			// message at startup, not a mystery on the first task.
			return err
		}
	}

	d.buildRunners(configs)
	if err := d.writeInfo(configs); err != nil {
		return err
	}
	defer d.cleanup()

	// The publisher is awaited, not just cancelled. Without the wait, Run could
	// return — and its deferred cleanup could delete status.json — while the
	// publisher was still mid-write, which both resurrects the file corgi just
	// removed and races anything clearing the directory behind it. Registered
	// after the cleanup defer so it runs first: stop publishing, wait, then
	// remove.
	publishCtx, stopPublishing := context.WithCancel(ctx)
	// Paired with the context so a later early return cannot skip it; the
	// ordered stop-then-wait below still does the real work.
	defer stopPublishing()
	publishDone := make(chan struct{})
	go func() {
		defer close(publishDone)
		d.publishStatus(publishCtx)
	}()
	defer func() {
		stopPublishing()
		<-publishDone
	}()

	var wg sync.WaitGroup
	for _, r := range d.Runners() {
		wg.Add(1)
		go func(r *supervisor.Runner) {
			defer wg.Done()
			if err := r.Run(ctx); err != nil && ctx.Err() == nil {
				utils.Infof("agent: %s stopped: %v\n", r.Config.WorkspaceID, err)
			}
		}(r)
	}
	wg.Wait()

	// Every supervisor has finished. If that was a deliberate disable — an auth
	// failure, or repeated startup failures — exiting here would hand the
	// decision to launchd or systemd, which would restart corgi and undo it.
	// Stay up instead, reporting the disabled state, until asked to stop.
	if ctx.Err() == nil {
		utils.Info("agent: every workspace is disabled — staying up so `corgi agent status` can explain why")
		// Nudge the publisher rather than writing here: both use the same
		// status.json.tmp, and a torn file would make `corgi agent status` fail
		// to parse exactly when it is needed to explain the disabled workspace.
		d.requestPublish()
		<-ctx.Done()
	}
	return ctx.Err()
}

func (d *Daemon) rememberAutostart(configs []supervisor.SpawnConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.autostart = make(map[string]supervisor.SpawnConfig, len(configs))
	for _, cfg := range configs {
		d.autostart[cfg.WorkspaceID] = cfg
	}
}

func (d *Daemon) buildRunners(configs []supervisor.SpawnConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runners = nil
	d.diags = nil
	env := os.Environ()

	for _, cfg := range configs {
		d.runners = append(d.runners, d.newRunner(cfg))
		d.diags = append(d.diags, diagnose(cfg, env))
	}
}

// newRunner wires one supervisor to the daemon's notifications, timeline and
// handover brief. The one place that wiring lives, so a runner started at
// boot and one started from a phone cannot differ.
func (d *Daemon) newRunner(cfg supervisor.SpawnConfig) *supervisor.Runner {
	lock := supervisor.NewWakeLock(cfg.WakeLockMode())
	r := supervisor.NewRunner(cfg, d.Start, lock)
	r.Notify = d.Notify
	r.OnChange = d.requestPublish
	r.OnSessionEnd = d.sessionEndHook(cfg, r)
	r.OnEvent = d.recordEvent(cfg.WorkspaceID)
	return r
}

// sessionEndHook writes the handover brief for one workspace.
//
// A relaunched session is a NEW session with none of the previous one's
// context. corgi cannot restore the conversation, but the branches and
// uncommitted work it left on disk are still there, and saying so is the
// difference between a restart costing an hour and costing nothing.
func (d *Daemon) sessionEndHook(cfg supervisor.SpawnConfig, r *supervisor.Runner) func(supervisor.Decision) string {
	if d.CaptureBrief == nil {
		return nil
	}
	return func(dec supervisor.Decision) string {
		b := d.CaptureBrief(brief.Params{
			WorkspaceID: cfg.WorkspaceID,
			Dir:         cfg.Dir,
			Cause:       string(dec.Cause),
			Reason:      dec.Reason,
			Restarts:    r.State().Restarts,
		})
		if b == nil {
			return ""
		}
		// Written even when Empty: the cause and reason always apply, and
		// skipping the write would make `corgi agent brief <id>` report "it has
		// not restarted" about a workspace that just did. Empty only decides
		// whether there is a summary line worth adding to the notification.
		//
		// A failed write is not worth failing a restart over.
		_ = brief.Write(d.Dir, *b)
		return b.Summary()
	}
}

// drainCommands refreshes the editor windows (a window's extension nudges
// after writing its record, and a command in the same drain may need it),
// executes every pending spool command, then publishes the board if
// anything changed.
func (d *Daemon) drainCommands(ctx context.Context, launch func(*supervisor.Runner)) {
	defer d.flushSessions()
	d.syncWindows()
	cmds, err := command.Drain(d.Dir, time.Now(), command.TTL)
	if err != nil {
		utils.Infof("agent: reading commands: %v\n", err)
		return
	}
	for _, c := range cmds {
		if ctx.Err() != nil {
			return
		}
		switch c.Action {
		case command.ActionStart:
			d.startWorkspace(ctx, c, launch)
		case command.ActionStop:
			d.stopRemoteWorkspace(ctx, c, launch)
		case command.ActionAttention:
			d.reportAttention(c)
		default:
			d.handleSessionCommand(ctx, c)
		}
	}
}

// reportAttention turns a hook's "this session wants a person" into a timeline
// entry and a notification. It never touches a runner: the session in question
// is usually one corgi does not supervise (a terminal or an editor).
func (d *Daemon) reportAttention(c command.Command) {
	detail := strings.TrimSpace(c.Detail)
	if detail == "" {
		detail = "a session is waiting for you"
	}
	d.Events.Append(c.WorkspaceID, events.Event{Kind: "attention", Reason: detail})
	if d.repeatedAttention(c.WorkspaceID, detail, time.Now()) {
		d.requestPublish()
		return
	}
	d.notifyAttention("corgi agent · "+c.WorkspaceID, detail, c.WorkspaceID)
	d.requestPublish()
}

// attentionRepeatWindow is how long the same message from the same
// workspace stays silent after it was sent once: Claude Code fires some
// notifications twice, and two sessions in one repo hitting the same
// prompt read as one thing to answer.
const attentionRepeatWindow = 10 * time.Minute

func (d *Daemon) repeatedAttention(workspaceID, detail string, now time.Time) bool {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	if d.recentAttention == nil {
		d.recentAttention = map[string]time.Time{}
	}
	key := workspaceID + "\x00" + detail
	for k, at := range d.recentAttention {
		if now.Sub(at) > attentionRepeatWindow {
			delete(d.recentAttention, k)
		}
	}
	if at, ok := d.recentAttention[key]; ok && now.Sub(at) <= attentionRepeatWindow {
		return true
	}
	d.recentAttention[key] = now
	return false
}

// needsPerson reads a notification's body for the things that wait on a
// person — a review asked for, a build gone red, a run that failed or
// stopped, a ticket blocked — as against what merely happened.
func needsPerson(body string) bool {
	b := strings.ToLower(body)
	for _, w := range []string{"review asked", "review requested", "went red", "build red", "failed", "blocked", "not started", "could not", "needs", "waiting on"} {
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

func (d *Daemon) notifyAttention(title, body, workspaceID string) {
	d.notifyAttentionAt(title, body, workspaceID, "")
}

// notifyAttentionAt is notifyAttention with somewhere better to go than the
// launcher: a tracker issue, a merge request. The launcher is the fallback,
// because a notification about one ticket should open that ticket.
func (d *Daemon) notifyAttentionAt(title, body, workspaceID, link string) {
	if link == "" && d.LinkFor != nil {
		link = d.LinkFor(workspaceID)
	}
	if d.Push != nil {
		data := map[string]string{"workspace": workspaceID}
		if link != "" {
			data["url"] = link
		}
		// What needs a person now, as against what happened: a phone with
		// quiet hours or "only what needs me" hears the first.
		if needsPerson(body) {
			data["needs"] = "1"
		}
		go d.Push(push.Message{Title: title, Body: body, Category: "inbox", Data: data, Thread: workspaceID})
	}
	if link != "" && d.NotifyWithLink != nil {
		d.NotifyWithLink(title, body, link)
		return
	}
	if d.Notify != nil {
		d.Notify(title, body)
	}
}

func (d *Daemon) startWorkspace(ctx context.Context, c command.Command, launch func(*supervisor.Runner)) {
	if d.isReplacing(c.WorkspaceID) {
		// A swap is under way — a phone Stop putting the device back, most
		// likely, with this Start a few seconds behind it. Dropping it would
		// leave the card on "ready" and the person tapping again; put it back
		// in the spool instead, same id and request time, so the next drain
		// retries it and the command's own TTL bounds the retries.
		_, _ = command.Write(d.Dir, c)
		return
	}
	if r := d.findRunner(c.WorkspaceID); r != nil && r.Supervising() {
		st := r.State()
		switch {
		case st.Running && st.DeviceOnly && st.SessionsThisRun == 0:
			// Online as a device and nobody has opened a session through it.
			// Start from the phone means "give me a conversation now", which
			// this process cannot: it was told not to open one. Swap it for a
			// server that does, once it is gone — two servers in one directory
			// would fight over the session record. Nothing is lost: there is
			// no session on it to lose.
			d.relaunchAfter(ctx, r, func() (supervisor.SpawnConfig, error) { return d.resolveRemote(c) },
				launch, func() { d.announceRemote(c, "session started remotely") },
				func(err error) { d.commandFailed(c, err) })
			return
		case st.Running:
			d.requestPublish() // already up — the fresh status is the answer
			return
		}
		// Supervising but not running: the runner is waiting out a restart
		// backoff after a failure. A remote start here is the user tapping
		// Retry — after fixing the cause (accepting the trust dialog, logging
		// in), they should not wait out a five-minute backoff, and silently
		// answering "already supervised" made the button look broken. Replace
		// the runner and try right now, with a fresh failure streak.
		r.StopAsync()
	}
	cfg, err := d.resolveRemote(c)
	if err != nil {
		d.commandFailed(c, err)
		return
	}
	if d.launchRunner(ctx, cfg, launch) {
		d.announceRemote(c, "session started remotely")
	}
}

// resolveRemote builds and validates the launch settings a spool command asks
// for. A remote start always opens a session: that is the whole request.
func (d *Daemon) resolveRemote(c command.Command) (supervisor.SpawnConfig, error) {
	cfg, err := d.ResolveWorkspace(c.WorkspaceID, c.Profile, c.Name)
	if err != nil {
		return supervisor.SpawnConfig{}, err
	}
	if cfg.Origin == "" {
		cfg.Origin = supervisor.OriginRemote
	}
	cfg.DeviceOnly = false
	if err := supervisor.ValidateSpawnConfig(cfg); err != nil {
		return supervisor.SpawnConfig{}, err
	}
	return cfg, nil
}

// launchRunner puts a new supervisor in place for cfg's workspace and starts
// it. False when the daemon is already shutting down: resolving can take long
// enough for cancel to land, and launching then would fire a "started"
// notification for a session that returns instantly and does not survive the
// restart.
func (d *Daemon) launchRunner(ctx context.Context, cfg supervisor.SpawnConfig, launch func(*supervisor.Runner)) bool {
	if ctx.Err() != nil {
		return false
	}
	r := d.newRunner(cfg)
	d.replaceRunner(r, diagnose(cfg, os.Environ()))
	launch(r)
	d.requestPublish()
	_ = d.writeInfoIDs(d.runnerIDs())
	return true
}

// relaunchAfter swaps a workspace's process for another: stops the current
// one, waits for it to be gone, then starts what resolve returns. The wait
// happens on its own goroutine so the command loop never sits out a teardown,
// and the workspace is marked as replacing so a second start in that window
// does not race the swap. The stop flag itself is set synchronously, so a
// stop later in the same drain batch sees the old runner as already going.
func (d *Daemon) relaunchAfter(ctx context.Context, old *supervisor.Runner, resolve func() (supervisor.SpawnConfig, error),
	launch func(*supervisor.Runner), started func(), failed func(error)) {
	id := old.Config.WorkspaceID
	d.mu.Lock()
	if d.replacing == nil {
		d.replacing = map[string]bool{}
	}
	d.replacing[id] = true
	d.mu.Unlock()
	old.StopAsync()
	d.swaps.Add(1)
	go func() {
		defer d.swaps.Done()
		defer func() {
			d.mu.Lock()
			delete(d.replacing, id)
			d.mu.Unlock()
		}()
		// Stop joins the teardown StopAsync began, so this returns only once
		// the process is down (or was never up).
		old.Stop()
		cfg, err := resolve()
		if err != nil {
			if failed != nil {
				failed(err)
			}
			return
		}
		if d.launchRunner(ctx, cfg, launch) && started != nil {
			started()
		}
	}()
}

func (d *Daemon) isReplacing(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.replacing[id]
}

func (d *Daemon) stopRemoteWorkspace(ctx context.Context, c command.Command, launch func(*supervisor.Runner)) {
	r := d.findRunner(c.WorkspaceID)
	if r == nil || !r.Supervising() {
		// A clean no-op, as the tool advertises. The id was resolved against the
		// registry before it reached the spool, so a missing or already-stopped
		// runner just means "not running" — not a failure to flash at the owner.
		return
	}
	// A workspace that came up with the daemon as a device goes back to being
	// one: Stop ends the session, it does not take the machine offline. A
	// device-only run with nothing on it is left alone — stopping it would
	// only put the same thing back.
	if auto, ok := d.autostartConfig(c.WorkspaceID); ok && auto.DeviceOnly {
		st := r.State()
		if st.DeviceOnly && st.SessionsThisRun == 0 {
			// Up, or between restarts: either way a device with nothing on it,
			// and a relaunch would only put the same thing back — and cut short
			// a backoff the runner is honouring.
			d.requestPublish()
			return
		}
		d.relaunchAfter(ctx, r, func() (supervisor.SpawnConfig, error) { return auto, nil }, launch, nil, nil)
		d.announceRemote(c, "session stopped remotely · back online as a device")
		d.requestPublish()
		return
	}
	// StopAsync sets the stop flag synchronously — so a start later in this same
	// drain batch sees Supervising()==false and is not deduplicated against a
	// runner on its way out — then backgrounds the blocking teardown (up to
	// stopGrace) so a batch of stops cannot stall the loop and age queued
	// commands past their TTL.
	r.StopAsync()
	d.announceRemote(c, "session stopped remotely")
	d.requestPublish()
}

func (d *Daemon) autostartConfig(id string) (supervisor.SpawnConfig, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, ok := d.autostart[id]
	return cfg, ok
}

// commandFailed surfaces a rejected command where a phone will see it: the
// diagnostics in status.json, plus a desktop notification.
func (d *Daemon) commandFailed(c command.Command, err error) {
	utils.Infof("agent: %s %s: %v\n", c.Action, c.WorkspaceID, err)
	d.setDiag(WorkspaceDiagnostic{WorkspaceID: c.WorkspaceID, Warning: fmt.Sprintf("remote %s failed: %v", c.Action, err)})
	if d.Notify != nil {
		d.Notify("corgi agent · "+c.WorkspaceID, fmt.Sprintf("remote %s failed: %v", c.Action, err))
	}
	d.requestPublish()
}

func (d *Daemon) announceRemote(c command.Command, what string) {
	if d.Notify == nil {
		return
	}
	body := what
	if c.Profile != "" {
		body += " · profile " + c.Profile
	}
	if c.Source != "" {
		body += " · via " + c.Source
	}
	d.Notify("corgi agent · "+c.WorkspaceID, body)
}

func (d *Daemon) findRunner(id string) *supervisor.Runner {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.runners {
		if r.Config.WorkspaceID == id {
			return r
		}
	}
	return nil
}

// replaceRunner swaps the entry for its workspace (or appends), keeping one
// runner and one diagnostic per workspace id.
func (d *Daemon) replaceRunner(r *supervisor.Runner, diag WorkspaceDiagnostic) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, old := range d.runners {
		if old.Config.WorkspaceID == r.Config.WorkspaceID {
			d.runners[i] = r
			d.setDiagLocked(diag)
			return
		}
	}
	d.runners = append(d.runners, r)
	d.setDiagLocked(diag)
}

func (d *Daemon) setDiag(diag WorkspaceDiagnostic) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.setDiagLocked(diag)
}

func (d *Daemon) setDiagLocked(diag WorkspaceDiagnostic) {
	for i, old := range d.diags {
		if old.WorkspaceID == diag.WorkspaceID {
			d.diags[i] = diag
			return
		}
	}
	d.diags = append(d.diags, diag)
}

func (d *Daemon) runnerIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make([]string, 0, len(d.runners))
	for _, r := range d.runners {
		ids = append(ids, r.Config.WorkspaceID)
	}
	return ids
}

// Runners returns the live supervisors.
func (d *Daemon) SessionURLFor(workspaceID string) string {
	r := d.findRunner(workspaceID)
	if r == nil {
		return ""
	}
	return r.State().SessionURL
}

func (d *Daemon) Runners() []*supervisor.Runner {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]*supervisor.Runner(nil), d.runners...)
}

// Status reports what every supervisor is doing.
func (d *Daemon) Status() Status {
	d.mu.Lock()
	diags := append([]WorkspaceDiagnostic(nil), d.diags...)
	d.mu.Unlock()

	s := Status{
		Running:      true,
		PID:          os.Getpid(),
		Version:      d.Version,
		WakeLockable: supervisor.Supported(),
		Diagnostics:  diags,
	}
	for _, r := range d.Runners() {
		s.Workspaces = append(s.Workspaces, r.State())
	}
	sort.Slice(s.Workspaces, func(i, j int) bool {
		return s.Workspaces[i].WorkspaceID < s.Workspaces[j].WorkspaceID
	})
	return s
}

// diagnose builds the per-workspace line that says which account will actually
// be used. An ambient ANTHROPIC_API_KEY is called out explicitly: remote
// control refuses to run with one set, and it silently bills the API.
func diagnose(cfg supervisor.SpawnConfig, env []string) WorkspaceDiagnostic {
	bin, _ := supervisor.ResolveBin(cfg)
	kind := cfg.Kind
	if kind == "" {
		kind = supervisor.DefaultKind
	}
	d := WorkspaceDiagnostic{
		WorkspaceID: cfg.WorkspaceID,
		Dir:         cfg.Dir,
		Kind:        kind,
		Bin:         bin,
		ConfigDir:   cfg.ConfigDir,
		Spawn:       cfg.Spawn,
		Stripped:    supervisor.StrippedCredentials(cfg, env),
	}
	if d.ConfigDir == "" {
		d.ConfigDir = "<default>"
	}
	if cfg.InheritAPIKey {
		d.Warning = "inheriting ANTHROPIC_API_KEY — remote control refuses to start with one set, and it bills the API rather than a subscription"
	}
	return d
}

// writeInfo records the daemon so other corgi processes can find it.
func (d *Daemon) writeInfo(configs []supervisor.SpawnConfig) error {
	ids := make([]string, 0, len(configs))
	for _, c := range configs {
		ids = append(ids, c.WorkspaceID)
	}
	return d.writeInfoIDs(ids)
}

func (d *Daemon) writeInfoIDs(ids []string) error {
	exe, _ := os.Executable()
	if d.startedAt.IsZero() {
		d.startedAt = time.Now().UTC()
	}
	return writeJSONAtomic(d.InfoPath(), Info{
		PID: os.Getpid(), Version: d.Version, Executable: exe,
		StartedAt: d.startedAt, Workspaces: ids,
		Commands: d.ResolveWorkspace != nil,
	})
}

// cleanup removes the daemon's published files so a stopped daemon never
// looks like a running one.
func (d *Daemon) cleanup() {
	_ = os.Remove(d.InfoPath())
	_ = os.Remove(d.StatusPath())
}

// ReadInfo returns the running daemon's record, or nil when none is running.
// A record whose process is gone is treated as absent and cleaned up, so a
// machine that lost power does not look like it still has a daemon.
func ReadInfo(dir string) (*Info, error) {
	path := filepath.Join(dir, "daemon.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	if info.PID <= 0 || !processAlive(info.PID) || !processMatchesRecord(info) {
		// Either gone, or the pid has been recycled by something else. Treating
		// it as absent is the safe reading: the alternative is signalling an
		// unrelated process.
		_ = os.Remove(path)
		return nil, nil
	}
	return &info, nil
}

// processMatchesRecord checks the pid is still running the binary that wrote
// the record. Pids are recycled, so without this a stale daemon.json makes
// `agent stop` SIGTERM an unrelated process and `agent serve` refuse to start.
func processMatchesRecord(info Info) bool {
	name, ok := processName(info.PID)
	if !ok {
		return true // cannot tell on this platform; do not invent a failure
	}
	if info.Executable == "" {
		// Written by an older version. Fall back to the product name.
		return strings.Contains(strings.ToLower(name), "corgi")
	}
	return strings.EqualFold(name, filepath.Base(info.Executable))
}

// processAlive is a plain liveness check. Unlike utils.PidAlive it does not
// require the process to be a group leader: the daemon is normally started by
// launchd or a shell, so it is usually not one. The probe itself differs by
// platform — see signal_unix.go and signal_windows.go.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAliveOS(pid)
}

// itoa avoids a strconv import in the small platform files.
func itoa(n int) string { return fmt.Sprint(n) }

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	// 0700/0600: status.json now carries each session's claude.ai URL, and
	// daemon.json the daemon's pid and paths — owner-only, like the spool.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

// digestMarker remembers the day the digest last went out, across restarts.
func digestMarker(dir string) string { return filepath.Join(dir, "digest.sent") }

// DigestDue says whether a digest configured for at ("HH:MM", local) should
// go out now, given the last day one was sent ("2006-01-02" or ""). Due
// from that minute until midnight, once: a daemon that was asleep at 20:00
// still sends at 20:07, and never twice.
func DigestDue(now time.Time, at, lastDay string) bool {
	at = strings.TrimSpace(at)
	if at == "" {
		return false
	}
	t, err := time.ParseInLocation("15:04", at, now.Location())
	if err != nil {
		return false
	}
	today := now.Format("2006-01-02")
	if lastDay == today {
		return false
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	return !now.Before(due)
}

func (d *Daemon) sendDigestIfDue(now time.Time) {
	if d.DigestAt == "" || d.Digest == nil || d.Notify == nil {
		return
	}
	last, _ := os.ReadFile(digestMarker(d.Dir))
	if !DigestDue(now, d.DigestAt, strings.TrimSpace(string(last))) {
		return
	}
	// Mark first: a digest that fails to send is not worth a retry storm.
	_ = os.WriteFile(digestMarker(d.Dir), []byte(now.Format("2006-01-02")), 0o600)
	body := strings.TrimSpace(d.Digest(now))
	if body == "" {
		return
	}
	d.Notify("corgi agent · today", body)
}

// awakeGrace is how long the machine stays awake after the last session
// stopped working. Long enough that a pause between turns, or a build that
// prints nothing for a minute, does not drop the lock mid-task.
const awakeGrace = 5 * time.Minute

// anyoneWorking says a tracked Claude is mid-turn right now — any of them,
// not only the ones corgi started. A session someone opened in a terminal
// keeps the laptop awake exactly as much as a supervised one.
func (d *Daemon) anyoneWorking() bool {
	if d.Sessions == nil {
		return false
	}
	return anyWorking(d.Sessions.Sessions())
}

// anyWorking is the test itself: mid-turn, not merely open. A session waiting
// on a person is not work, and letting it hold the lock is how a laptop ends
// up awake all night.
func anyWorking(list []sessions.Session) bool {
	for _, s := range list {
		if s.Status == sessions.StatusWorking {
			return true
		}
	}
	return false
}

// holdAwakeWhileWorking keeps the machine from sleeping while any tracked
// session is working, and lets it sleep once they have all been quiet for
// awakeGrace. The per-workspace lock only ever covered corgi's own supervised
// processes, so a session started by hand in a terminal would be cut off
// mid-turn by the lid closing — which is why people ran caffeinate themselves.
// HoldAwakeWhileWorking is holdAwakeWhileWorking, exported for the command
// layer that owns the lock.
func (d *Daemon) HoldAwakeWhileWorking(ctx context.Context, lock *supervisor.WakeLock) {
	if lock == nil {
		return
	}
	defer lock.Release()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastBusy := time.Time{}
	for {
		if d.anyoneWorking() {
			lastBusy = time.Now()
			if !lock.Held() {
				if err := lock.Acquire(os.Getpid()); err != nil {
					utils.Infof("agent: could not hold the machine awake: %v\n", err)
				}
			}
		} else if lock.Held() && !lastBusy.IsZero() && time.Since(lastBusy) > awakeGrace {
			lock.Release()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
