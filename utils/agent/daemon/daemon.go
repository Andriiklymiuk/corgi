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
	// CaptureBrief probes what an ending session left on disk. Injected because
	// enumerating a stack's repositories means parsing a compose file, which the
	// daemon has no business knowing about. Nil disables briefs entirely.
	CaptureBrief func(brief.Params) *brief.Brief

	mu      sync.Mutex
	runners []*supervisor.Runner
	diags   []WorkspaceDiagnostic

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
		publishSignal: make(chan struct{}, 1),
	}
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
func (d *Daemon) Run(ctx context.Context, configs []supervisor.SpawnConfig) error {
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

	publishCtx, stopPublishing := context.WithCancel(ctx)
	defer stopPublishing()
	go d.publishStatus(publishCtx)

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
	exe, _ := os.Executable()
	info := Info{PID: os.Getpid(), Version: d.Version, Executable: exe, StartedAt: time.Now().UTC()}
	for _, c := range configs {
		info.Workspaces = append(info.Workspaces, c.WorkspaceID)
	}
	return writeJSONAtomic(d.InfoPath(), info)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
