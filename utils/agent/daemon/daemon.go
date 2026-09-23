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
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type Info struct {
	PID        int       `json:"pid"`
	Version    string    `json:"version"`
	Executable string    `json:"executable,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	Workspaces []string  `json:"workspaces"`
	// Older daemons have no SIGUSR1 handler; a nudge would kill them, so Nudge checks this first.
	Commands bool `json:"commands,omitempty"`
}

type Status struct {
	Running      bool                  `json:"running"`
	PID          int                   `json:"pid,omitempty"`
	StartedAt    time.Time             `json:"startedAt,omitempty"`
	Version      string                `json:"version,omitempty"`
	WakeLockable bool                  `json:"wakeLockSupported"`
	Workspaces   []supervisor.RunState `json:"workspaces"`
	Diagnostics  []WorkspaceDiagnostic `json:"diagnostics,omitempty"`
}

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

type Daemon struct {
	attentionMu     sync.Mutex
	recentAttention map[string]time.Time
	limitWatch      map[string]bool
	LiftGrace       time.Duration

	PulseURL   string
	PulseEvery time.Duration

	Watches     []WatchSpec
	watchState  *watch.State
	watchers    map[string]*watch.Watch
	fixBusy     map[string]chan struct{}
	carried     map[string]time.Time
	rungClaims  map[string]bool
	headlessMu  sync.Mutex
	headless    map[string]bool
	fixActive   map[string]bool
	pruned      map[string]bool
	lastPrune   time.Time
	liftRang    map[string]time.Time
	liftDue     map[string]*time.Timer
	liftTold    map[string]bool
	fixSettle   map[string]*time.Timer
	fixSettled  map[string][]watch.Event
	fixBatch    map[string][]watch.Event
	fixBatchAt  map[string]*time.Timer
	fixFollowUp map[string]watch.Event
	gateMu      sync.Mutex
	gating      map[string]bool

	Version          string
	Dir              string
	Start            supervisor.Starter
	Notify           func(title, body string)
	NotifyWithLink   func(title, body, link string)
	NotifyFocus      func(title, body, sessionID, link string)
	LinkFor          func(workspaceID string) string
	Pickup           func(workspace string, e watch.Event)
	ClaimTicket      func(workspace string, e watch.Event) (ok bool, holder string, err error)
	Delivered        func(workspace string, e watch.Event, prs []string)
	Workpad          func(workspace, ref, section, text string)
	Push             func(m push.Message)
	Chat             func(ctx context.Context, workspace string, target watch.SlackTarget, text, emoji string) error
	Isolate          func(dir, branch string) ([]string, error)
	Events           *events.Log
	CaptureBrief     func(brief.Params) *brief.Brief
	peer             *peerNotes
	peerOnce         sync.Once
	ResolveWorkspace func(workspaceID, profile, name string) (supervisor.SpawnConfig, error)
	CommandTick      time.Duration
	IdleTick         time.Duration

	Sessions       *sessions.Registry
	Ledger         *usage.Ledger
	MergePull      func(ctx context.Context, workspace, link string) error
	MoveTicket     func(ctx context.Context, workspace, ref, status string) error
	RerunCI        func(ctx context.Context, workspace, repo string, since time.Time) (watch.Rerun, error)
	Policy         func(s sessions.Session) Policy
	Carry          func(s sessions.Session) (string, error)
	Shell          func(ctx context.Context, dir, cmd string) ([]byte, error)
	Raise          func(ctx context.Context, t sessions.FocusTarget) error
	Alive          func(pid int) bool
	ListProcesses  func() ([]proc.Process, error)
	Cwd            func(pid int) string
	ReapTick       time.Duration
	AccountDirs    func() []string
	TypeText       func(ctx context.Context, t sessions.FocusTarget, text string, enter bool) error
	AutoContinue   bool
	SessionCap     int64
	spent          map[string]spendMark
	DigestAt       string
	Digest         func(now time.Time) string
	publishStopped func()

	mu        sync.Mutex
	runners   []*supervisor.Runner
	diags     []WorkspaceDiagnostic
	startedAt time.Time
	autostart map[string]supervisor.SpawnConfig
	replacing map[string]bool
	swaps     sync.WaitGroup
	runs      sync.WaitGroup
	routines  *routineState

	nudge chan struct{}

	publishSignal chan struct{}
	reapSignal    chan struct{}
}

func New(version, dir string) *Daemon {
	return &Daemon{
		Version:        version,
		Dir:            dir,
		Start:          supervisor.StartProcess,
		Notify:         utils.Notify,
		NotifyWithLink: utils.NotifyWithLink,
		Events:         events.NewLog(dir),
		Sessions:       sessions.New(SessionsPath(dir), sessions.DefaultSize),
		Ledger:         usage.OpenLedger(dir),
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

func (d *Daemon) Nudge() {
	select {
	case d.nudge <- struct{}{}:
	default:
	}
}

// Only signals a daemon that advertised Commands: SIGUSR1 kills one without a handler.
func Nudge(info *Info) {
	if info == nil || info.PID <= 0 || !info.Commands {
		return
	}
	_ = nudgeProcess(info.PID)
}

func (d *Daemon) InfoPath() string { return filepath.Join(d.Dir, "daemon.json") }

func (d *Daemon) StatusPath() string { return filepath.Join(d.Dir, "status.json") }

var (
	statusPublishInterval = 5 * time.Second
	idleInterval          = time.Minute
	digestCheckInterval   = time.Minute
)

func (d *Daemon) requestPublish() {
	if d.publishSignal == nil {
		return
	}
	select {
	case d.publishSignal <- struct{}{}:
	default:
	}
}

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

func (d *Daemon) wakeLoops() {
	d.requestPublish()
	select {
	case d.reapSignal <- struct{}{}:
	default:
	}
}

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

func ReadStatus(dir string) (*Status, error) {
	info, err := ReadInfo(dir)
	if err != nil || info == nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if os.IsNotExist(err) {
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

func (d *Daemon) Run(ctx context.Context, configs []supervisor.SpawnConfig) error {
	// Catch SIGUSR1 for the whole lifetime: unhandled, it terminates the process.
	stopSignals := notifyNudge(d.nudge)
	defer stopSignals()

	if d.ResolveWorkspace != nil {
		return d.runDynamic(ctx, configs)
	}
	return d.runFixed(ctx, configs)
}

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
		utils.Info("agent: no autostart workspaces - waiting for remote session starts")
	}

	d.startSessionTracking()
	d.backfillLedger(ctx)
	d.startWatches(ctx)
	reapDone := make(chan struct{})
	go func() { defer close(reapDone); d.reapSessions(ctx) }()
	defer func() { <-reapDone }()
	pulseDone := make(chan struct{})
	go func() { defer close(pulseDone); d.pulse(ctx) }()
	defer func() { <-pulseDone }()
	peersDone := make(chan struct{})
	go func() { defer close(peersDone); d.pulsePeers(ctx) }()
	defer func() { <-peersDone }()

	ticker := time.NewTicker(d.pollInterval(d.CommandTick))
	defer ticker.Stop()

	idle := d.idle()
	for ctx.Err() == nil {
		d.reassertInfo()
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
	d.swaps.Wait()
	wg.Wait()
	return ctx.Err()
}

func (d *Daemon) runFixed(ctx context.Context, configs []supervisor.SpawnConfig) error {
	if len(configs) == 0 {
		return fmt.Errorf("no workspaces configured for agent mode - run `corgi agent init` in a stack, or `corgi agent scan <dir>`")
	}

	for _, cfg := range configs {
		if err := supervisor.ValidateSpawnConfig(cfg); err != nil {
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

	if ctx.Err() == nil {
		utils.Info("agent: every workspace is disabled - staying up so `corgi agent status` can explain why")
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

func (d *Daemon) newRunner(cfg supervisor.SpawnConfig) *supervisor.Runner {
	lock := supervisor.NewWakeLock(cfg.WakeLockMode())
	r := supervisor.NewRunner(cfg, d.Start, lock)
	r.Notify = d.Notify
	r.OnChange = d.requestPublish
	r.OnSessionEnd = d.sessionEndHook(cfg, r)
	r.OnEvent = d.recordEvent(cfg.WorkspaceID)
	return r
}

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
		_ = brief.Write(d.Dir, *b)
		return b.Summary()
	}
}

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
	d.notifyAttention(notifyTitlePrefix+c.WorkspaceID, detail, c.WorkspaceID)
	d.requestPublish()
}

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

func needsPerson(body string) bool {
	b := strings.ToLower(body)
	for _, w := range []string{"review asked", "review requested", "went red", "build red", "failed", "blocked", "not started", "could not", "needs", "waiting on"} {
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

const notifyTitlePrefix = "corgi agent · "

func (d *Daemon) notifyAttention(title, body, workspaceID string) {
	d.notifyAttentionAt(title, body, workspaceID, "")
}

func (d *Daemon) notifyAttentionAt(title, body, workspaceID, link string) {
	d.notifyAttentionKey(title, body, workspaceID, link, "")
}

func (d *Daemon) notifySession(title, body string, s sessions.Session) {
	d.notifyAttentionFull(title, body, s.Folder, "", "", s.ID)
}

func (d *Daemon) notifyAttentionKey(title, body, workspaceID, link, key string) {
	d.notifyAttentionFull(title, body, workspaceID, link, key, "")
}

func (d *Daemon) notifyAttentionFull(title, body, workspaceID, link, key, sessionID string) {
	if d.muted() {
		utils.Infof("agent: (muted) %s: %s\n", title, body)
		return
	}
	if d.silenced(workspaceID) {
		utils.Infof("agent: (silent %s) %s: %s\n", workspaceID, title, body)
		return
	}
	if link == "" && d.LinkFor != nil {
		link = d.LinkFor(workspaceID)
	}
	if d.Push != nil {
		data := map[string]string{"workspace": workspaceID}
		if link != "" {
			data["url"] = link
		}
		if key != "" {
			data["key"] = key
		}
		if needsPerson(body) {
			data["needs"] = "1"
		}
		go d.Push(push.Message{Title: title, Body: body, Category: "inbox", Data: data, Thread: workspaceID})
	}
	if d.NotifyFocus != nil {
		if id := d.focusableSession(sessionID, workspaceID); id != "" {
			d.NotifyFocus(title, body, id, link)
			return
		}
	}
	if link != "" && d.NotifyWithLink != nil {
		d.NotifyWithLink(title, body, link)
		return
	}
	if d.Notify != nil {
		d.Notify(title, body)
	}
}

func (d *Daemon) focusableSession(sessionID, workspaceID string) string {
	if d.Sessions == nil {
		return ""
	}
	var best sessions.Session
	for _, s := range d.Sessions.Sessions() {
		if !inWindow(s) {
			continue
		}
		if sessionID != "" {
			if s.ID == sessionID {
				return s.ID
			}
			continue
		}
		if workspaceID == "" || !sessions.Within(s.Cwd, workspaceID) && s.Folder != workspaceID {
			continue
		}
		if best.ID == "" || s.LastActivity.After(best.LastActivity) {
			best = s
		}
	}
	return best.ID
}

func inWindow(s sessions.Session) bool {
	if s.Status == sessions.StatusGone {
		return false
	}
	switch s.Host.Kind {
	case sessions.HostVSCodeTerminal, sessions.HostVSCodePanel, sessions.HostITerm, sessions.HostTerminalApp, sessions.HostTmux:
		return true
	}
	return false
}

func (d *Daemon) silenced(workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	for _, spec := range d.Watches {
		if !spec.Silent {
			continue
		}
		if spec.Workspace == workspaceID {
			return true
		}
		if spec.Dir != "" && (workspaceID == spec.Dir || strings.HasPrefix(workspaceID, strings.TrimRight(spec.Dir, "/")+"/")) {
			return true
		}
	}
	return false
}

func (d *Daemon) startWorkspace(ctx context.Context, c command.Command, launch func(*supervisor.Runner)) {
	if d.isReplacing(c.WorkspaceID) {
		_, _ = command.Write(d.Dir, c)
		return
	}
	if r := d.findRunner(c.WorkspaceID); r != nil && r.Supervising() {
		st := r.State()
		switch {
		case st.Running && st.DeviceOnly && st.SessionsThisRun == 0:
			d.relaunchAfter(ctx, r, func() (supervisor.SpawnConfig, error) { return d.resolveRemote(c) },
				launch, func() { d.announceRemote(c, "session started remotely") },
				func(err error) { d.commandFailed(c, err) })
			return
		case st.Running:
			d.requestPublish()
			return
		}
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
		return
	}
	if auto, ok := d.autostartConfig(c.WorkspaceID); ok && auto.DeviceOnly {
		st := r.State()
		if st.DeviceOnly && st.SessionsThisRun == 0 {
			d.requestPublish()
			return
		}
		d.relaunchAfter(ctx, r, func() (supervisor.SpawnConfig, error) { return auto, nil }, launch, nil, nil)
		d.announceRemote(c, "session stopped remotely · back online as a device")
		d.requestPublish()
		return
	}
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

func (d *Daemon) commandFailed(c command.Command, err error) {
	utils.Infof("agent: %s %s: %v\n", c.Action, c.WorkspaceID, err)
	d.setDiag(WorkspaceDiagnostic{WorkspaceID: c.WorkspaceID, Warning: fmt.Sprintf("remote %s failed: %v", c.Action, err)})
	if d.Notify != nil {
		d.Notify(notifyTitlePrefix+c.WorkspaceID, fmt.Sprintf("remote %s failed: %v", c.Action, err))
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
	d.Notify(notifyTitlePrefix+c.WorkspaceID, body)
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
		d.Warning = "inheriting ANTHROPIC_API_KEY - remote control refuses to start with one set, and it bills the API rather than a subscription"
	}
	return d
}

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

func (d *Daemon) cleanup() {
	_ = d.Ledger.Flush()
	d.removeOwnInfo()
	_ = os.Remove(d.StatusPath())
}

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
		_ = os.Remove(path)
		return nil, nil
	}
	return &info, nil
}

func processMatchesRecord(info Info) bool {
	name, ok := processName(info.PID)
	if !ok {
		return true
	}
	if info.Executable == "" {
		return strings.Contains(strings.ToLower(name), "corgi")
	}
	return strings.EqualFold(name, filepath.Base(info.Executable))
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAliveOS(pid)
}

func itoa(n int) string { return fmt.Sprint(n) }

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

func digestMarker(dir string) string { return filepath.Join(dir, "digest.sent") }

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
	_ = os.WriteFile(digestMarker(d.Dir), []byte(now.Format("2006-01-02")), 0o600)
	body := strings.TrimSpace(d.Digest(now))
	if body == "" {
		return
	}
	d.Notify("corgi agent · today", body)
	if d.Push != nil {
		lines := strings.Split(body, "\n")
		if len(lines) > 3 {
			lines = append(lines[:3], "…")
		}
		go d.Push(push.Message{Title: "corgi agent · today", Body: strings.Join(lines, "\n"), Category: "brief", Data: map[string]string{"brief": "1"}, Thread: "brief"})
	}
}

const awakeGrace = 5 * time.Minute

func (d *Daemon) anyoneWorking() bool {
	if d.Sessions == nil {
		return false
	}
	return anyWorking(d.Sessions.Sessions())
}

func anyWorking(list []sessions.Session) bool {
	for _, s := range list {
		if s.Status == sessions.StatusWorking {
			return true
		}
	}
	return false
}

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
