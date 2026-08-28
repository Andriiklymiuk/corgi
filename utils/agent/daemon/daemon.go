// Package daemon runs one supervisor per workspace and reports what they are
// doing. It owns no session state of its own: Remote Control owns sessions,
// corgi owns keeping Remote Control up.
package daemon

import (
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
	"andriiklymiuk/corgi/utils/agent/supervisor"
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
	Version string
	// Dir is the agent data directory holding daemon.json and registry.json.
	Dir string
	// Start launches a remote-control process; injected for tests.
	Start supervisor.Starter
	// Notify reports restarts. Defaults to corgi's desktop notification.
	Notify func(title, body string)
	Events *events.Log
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

	// nudge wakes the command loop; the cross-process doorbell is SIGUSR1.
	nudge chan struct{}

	// publishSignal is nudged whenever a supervisor's state changes, so
	// `corgi agent status` in another process is not up to five seconds stale.
	publishSignal chan struct{}
}

// New returns a Daemon writing state under dir.
func New(version, dir string) *Daemon {
	return &Daemon{
		Version:       version,
		Dir:           dir,
		Start:         supervisor.StartProcess,
		Notify:        utils.Notify,
		Events:        events.NewLog(dir),
		publishSignal: make(chan struct{}, 1),
		nudge:         make(chan struct{}, 1),
	}
}

func (d *Daemon) recordEvent(workspaceID string) func(supervisor.RunEvent) {
	return func(e supervisor.RunEvent) {
		d.Events.Append(workspaceID, events.Event{
			Kind: e.Kind, PID: e.PID, Cause: e.Cause, Reason: e.Reason, URL: e.URL,
		})
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

// Nudge pokes a running daemon in another process so it drains the spool now
// rather than on the next tick. Best-effort: the tick still wins without it.
//
// It signals ONLY a daemon that advertised command support. A daemon from
// before this feature has no handler for SIGUSR1, whose default disposition is
// to terminate — so nudging one would kill it. Such a daemon cannot drain the
// spool anyway; the caller should tell the user to restart it.
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

// statusPublishInterval is how often the running daemon republishes its state.
// `corgi agent status` runs in a different process, so a file is the simplest
// thing that works — no socket, no port, nothing to secure.
const statusPublishInterval = 5 * time.Second

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

// publishStatus keeps status.json fresh until ctx ends. It republishes on every
// state change, with a slow tick as a safety net for anything that changes
// without notifying (the wake lock, for instance).
func (d *Daemon) publishStatus(ctx context.Context) {
	if d.publishStopped != nil {
		defer d.publishStopped()
	}
	ticker := time.NewTicker(statusPublishInterval)
	defer ticker.Stop()
	for {
		_ = writeJSONAtomic(d.StatusPath(), d.Status())
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

// Run supervises every config until ctx is cancelled.
//
// Each workspace gets its own goroutine; one workspace disabling itself does
// not take the others down. Run returns only when every supervisor has
// finished, so a caller can rely on it meaning "nothing of mine is still up".
//
// With ResolveWorkspace set, Run also drains the command spool and can start
// and stop workspaces at runtime; without it, the fixed-set lifecycle below is
// unchanged.
func (d *Daemon) Run(ctx context.Context, configs []supervisor.SpawnConfig) error {
	// Catch SIGUSR1 for the daemon's ENTIRE lifetime, before writeInfo makes
	// this pid discoverable to a nudge and until after cleanup. Go's default
	// disposition for SIGUSR1 is to terminate the process, so a nudge landing
	// in the window before the handler was installed — a phone start racing a
	// launchd restart, exactly what this feature invites — would kill the
	// daemon outright. Installed on the fixed path too, where nothing reads the
	// channel, purely so the signal is never fatal.
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

	tick := d.CommandTick
	if tick == 0 {
		tick = statusPublishInterval
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for ctx.Err() == nil {
		d.drainCommands(ctx, launch)
		select {
		case <-ctx.Done():
		case <-d.nudge:
		case <-ticker.C:
		}
	}
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

func (d *Daemon) buildRunners(configs []supervisor.SpawnConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runners = nil
	d.diags = nil
	env := os.Environ()

	for _, cfg := range configs {
		lock := supervisor.NewWakeLock(cfg.WakeLockMode())
		r := supervisor.NewRunner(cfg, d.Start, lock)
		r.Notify = d.Notify
		r.OnChange = d.requestPublish
		r.OnSessionEnd = d.sessionEndHook(cfg, r)
		r.OnEvent = d.recordEvent(cfg.WorkspaceID)
		d.runners = append(d.runners, r)
		d.diags = append(d.diags, diagnose(cfg, env))
	}
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

// drainCommands executes every pending spool command.
func (d *Daemon) drainCommands(ctx context.Context, launch func(*supervisor.Runner)) {
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
			d.stopRemoteWorkspace(c)
		case command.ActionAttention:
			d.reportAttention(c)
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
	if d.Notify != nil {
		d.Notify("corgi agent · "+c.WorkspaceID, detail)
	}
	d.requestPublish()
}

func (d *Daemon) startWorkspace(ctx context.Context, c command.Command, launch func(*supervisor.Runner)) {
	if r := d.findRunner(c.WorkspaceID); r != nil && r.Supervising() {
		if r.State().Running {
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
	cfg, err := d.ResolveWorkspace(c.WorkspaceID, c.Profile, c.Name)
	if err != nil {
		d.commandFailed(c, err)
		return
	}
	if cfg.Origin == "" {
		cfg.Origin = supervisor.OriginRemote
	}
	if err := supervisor.ValidateSpawnConfig(cfg); err != nil {
		d.commandFailed(c, err)
		return
	}
	if ctx.Err() != nil {
		// The daemon is shutting down. Resolving took long enough for cancel to
		// land; launching now would fire a "started" notification for a session
		// that returns instantly and does not survive the restart.
		return
	}
	lock := supervisor.NewWakeLock(cfg.WakeLockMode())
	r := supervisor.NewRunner(cfg, d.Start, lock)
	r.Notify = d.Notify
	r.OnChange = d.requestPublish
	r.OnSessionEnd = d.sessionEndHook(cfg, r)
	r.OnEvent = d.recordEvent(cfg.WorkspaceID)
	d.replaceRunner(r, diagnose(cfg, os.Environ()))
	launch(r)
	d.announceRemote(c, "session started remotely")
	d.requestPublish()
	_ = d.writeInfoIDs(d.runnerIDs())
}

func (d *Daemon) stopRemoteWorkspace(c command.Command) {
	r := d.findRunner(c.WorkspaceID)
	if r == nil || !r.Supervising() {
		// A clean no-op, as the tool advertises. The id was resolved against the
		// registry before it reached the spool, so a missing or already-stopped
		// runner just means "not running" — not a failure to flash at the owner.
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

// processMatchesRecord checks that the pid is still running the binary that
// wrote the record.
//
// Pids are recycled. Without this, a daemon.json left behind by an unclean exit
// would make `corgi agent stop` SIGTERM whatever now holds that number, and
// `corgi agent serve` would refuse to start because it believes a daemon is
// already running.
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
	// 0700/0600: status.json now carries each session's claude.ai URL, and
	// daemon.json the daemon's pid and paths — owner-only, like the spool.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
