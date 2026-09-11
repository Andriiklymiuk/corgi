package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// corgi's own phone UI: after pairing the browser page lists workspaces and
// starts a session in one tap, no claude.ai connector needed.
//
//   GET  /app                 the launcher page (static; uses the stored token)
//   GET  /launch/workspaces   list workspaces with running state + sessionUrl
//   POST /launch/start        {workspace, profile?} → start a session
//
// /launch/* sits behind the same auth as /mcp and grants no new capability.

// launchWorkspace is one row in the launcher list.
type launchWorkspace struct {
	ID         string   `json:"id"`
	Aliases    []string `json:"aliases,omitempty"`
	Path       string   `json:"path"`
	Status     string   `json:"status"`
	Running    bool     `json:"running"`
	SessionURL string   `json:"sessionUrl,omitempty"`
	// SessionLinks are per-session claude.ai URLs captured from remote
	// control's own output — the only links the site resolves (the ids the
	// claude CLI lists locally are UUIDs the web does not know).
	SessionLinks []string `json:"sessionLinks,omitempty"`
	Note         string   `json:"note,omitempty"`
	// State is the one word the card leads with: what this workspace is doing
	// right now, decided here rather than in three places in the page.
	State string `json:"state"`
	// Everything from here down the daemon already knew and the phone could
	// not see: `corgi agent status` on the laptop said more than the page you
	// carry around.
	Disabled  bool   `json:"disabled,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	Restarts  int    `json:"restarts,omitempty"`
	Profile   string `json:"profile,omitempty"`
	WakeLock  bool   `json:"wakeLock,omitempty"`
	Origin    string `json:"origin,omitempty"`
	PID       int    `json:"pid,omitempty"`
	LastCause string `json:"lastCause,omitempty"`
	// DeviceOnly says the supervised server opened no session of its own: it
	// is online as a device, and Start on the card is what gives it one.
	DeviceOnly bool `json:"deviceOnly,omitempty"`
	// Remark is the daemon's standing note about how this workspace runs — a
	// flag the installed CLI did not know, say. Informational, unlike Note,
	// which is a refusal.
	Remark string `json:"remark,omitempty"`
	// Branch and Dirty describe the checkout a session here would start on —
	// the answer to "which of these two is the one I was working in?".
	Branch     string            `json:"branch,omitempty"`
	Dirty      bool              `json:"dirty,omitempty"`
	Live       int               `json:"live"`
	TopSession *launchTopSession `json:"topSession,omitempty"`
	Usage      *usage.Report     `json:"usage,omitempty"`
	LastEvent  *launchLastEvent  `json:"lastEvent,omitempty"`
	Profiles   []string          `json:"profiles,omitempty"`
}

type launchTopSession struct {
	Name string `json:"name"`
	// NameSource and NameSince are Claude Code's own record of where the
	// session's current name came from — "user" for one someone typed,
	// "derived"/"auto" for one Claude picked, "hook" for one a hook set — and
	// when it last changed. corgi shows the live name, so a session Claude has
	// since renamed reads as its new name here too.
	NameSource string `json:"nameSource,omitempty"`
	NameSince  int64  `json:"nameSince,omitempty"`
	// WaitingFor is what Claude Code says this session is blocked on, when it
	// says anything ("input needed", "dialog open", "sandbox request"). Absent
	// means it did not say, never that the session is idle.
	WaitingFor string `json:"waitingFor,omitempty"`
	Where      string `json:"where"`
	StartedAt  int64  `json:"startedAt,omitempty"`
	URL        string `json:"url,omitempty"`
}

type launchLastEvent struct {
	Kind   string `json:"kind"`
	Cause  string `json:"cause,omitempty"`
	Reason string `json:"reason,omitempty"`
	At     int64  `json:"at"`
}

func launchWorkspacesHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	registry, _, err := agentRegistry()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not read the workspace registry")
		return
	}
	registry.Reconcile(dirIsWorkspace)

	var status *daemon.Status
	if dir, derr := agentDir(); derr == nil {
		status, _ = daemon.ReadStatus(dir)
	}
	out := buildLaunchWorkspaces(registry, status)
	writeLaunchJSON(w, map[string]any{"workspaces": out})
}

type wsRunState struct {
	running   bool
	url       string
	note      string
	sessions  []string
	disabled  bool
	startedAt int64
	restarts  int
	profile   string
	wakeLock  bool
	origin    string
	pid       int
	lastCause string
	device    bool
	remark    string
}

// buildLaunchWorkspaces joins the registry with the daemon's live status into
// the launcher rows. A start the daemon refused (sensitive, unknown profile,
// unreachable, bad bin) leaves a diagnostic warning, not a run state — merging
// it in is what stops the phone showing "Starting…" then silently giving up.
func buildLaunchWorkspaces(registry *workspace.Registry, status *daemon.Status) []launchWorkspace {
	running := launchRunStates(status)
	profiles := launchProfileNames()
	out := make([]launchWorkspace, 0, len(registry.Workspaces))
	for _, ws := range registry.Sorted() {
		s := running[ws.ID]
		row := launchWorkspace{
			ID: ws.ID, Aliases: ws.Aliases, Path: ws.AbsPath,
			Status: string(ws.Status), Running: s.running, SessionURL: s.url,
			SessionLinks: s.sessions, Note: s.note, Profiles: profiles,
			Disabled: s.disabled, StartedAt: s.startedAt, Restarts: s.restarts,
			Profile: s.profile, WakeLock: s.wakeLock, Origin: s.origin,
			PID: s.pid, LastCause: s.lastCause,
			DeviceOnly: s.device, Remark: s.remark,
		}
		row.Branch, row.Dirty = workspaceCheckout(ws.ID, ws.AbsPath)
		row.Live, row.TopSession, row.LastEvent = workspaceActivity(ws.ID, ws.AbsPath, s.profile)
		row.Usage = workspaceUsage(ws.ID, ws.AbsPath, s.profile)
		row.State = launchState(row)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// launchRunStates flattens the daemon's published status into one record per
// workspace id, with a refused start's warning folded in as the note.
func launchRunStates(status *daemon.Status) map[string]wsRunState {
	running := map[string]wsRunState{}
	if status == nil {
		return running
	}
	for _, ws := range status.Workspaces {
		running[ws.WorkspaceID] = runStateOf(ws)
	}
	for _, d := range status.Diagnostics {
		if d.Warning == "" {
			continue
		}
		s := running[d.WorkspaceID]
		s.note = d.Warning
		running[d.WorkspaceID] = s
	}
	return running
}

func runStateOf(ws supervisor.RunState) wsRunState {
	started := int64(0)
	if !ws.StartedAt.IsZero() {
		started = ws.StartedAt.UnixMilli()
	}
	// Gated on Running: DeviceOnly describes the last run and survives its
	// exit, and a server sitting in a restart backoff is not "online".
	idleDevice := ws.Running && ws.DeviceOnly && ws.SessionsThisRun == 0
	url := ws.SessionURL
	if idleDevice {
		// Whatever link a device with no session printed, it is not a
		// conversation to open. The card's button must be Start.
		url = ""
	}
	return wsRunState{
		running: ws.Running, url: url, note: ws.LastReason, sessions: ws.Sessions,
		disabled: ws.Disabled, startedAt: started, restarts: ws.Restarts,
		profile: ws.Profile, wakeLock: ws.WakeLock, origin: ws.Origin,
		pid: ws.PID, lastCause: string(ws.LastCause),
		device: idleDevice, remark: ws.Note,
	}
}

// launchState reduces running, live sessions, the last event and a refused
// start to the one word the card leads with. Decided here so the phone and
// anything else reading /launch/workspaces agree on it. Finer than `corgi
// agent status`'s workspaceState, which answers a different question: whether
// the daemon is supervising, not whether a human is needed.
func launchState(row launchWorkspace) string {
	return launchStateAt(row, time.Now())
}

// launchReadyAfter is how long a device-only server gets to register before
// the card stops calling it "starting". Remote control is up within seconds;
// past this, running with no session is its resting state, not a start that
// never finished.
const launchReadyAfter = 15 * time.Second

func launchStateAt(row launchWorkspace, now time.Time) string {
	switch {
	case row.Disabled:
		// The daemon gave up on this one after repeated failures. It looked
		// exactly like "stopped" on the phone, and Start on a disabled
		// workspace is the tap that appears to do nothing.
		return "disabled"
	case row.Note != "" && !row.Running:
		// A start the daemon refused, or a diagnostic: the reason is on the card.
		return "blocked"
	case row.LastEvent != nil && row.LastEvent.Kind == "attention":
		// A permission prompt or a question is blocking the session — the one
		// state where the session is running and still needs a human.
		return "attention"
	case row.Live > 0:
		return "live"
	case row.Running && row.DeviceOnly && row.StartedAt > 0 &&
		now.Sub(time.UnixMilli(row.StartedAt)) >= launchReadyAfter:
		// Online as a device with no session: the machine answers, and Start
		// is what opens a conversation. Not "starting" — nothing is pending.
		return "ready"
	case row.Running:
		// Supervised, but no session has registered yet.
		return "starting"
	}
	return "stopped"
}

func workspaceActivity(id, absPath, profile string) (int, *launchTopSession, *launchLastEvent) {
	// The pid-file reader, never listClaudeSessions: its fallback shells out,
	// and this runs per workspace on a list the phone polls while starting.
	var live int
	var top *launchTopSession
	if _, configDir, ok := workspaceSessionTarget(id, profile); ok && absPath != "" {
		if sessions, read := localClaudeSessions(absPath, configDir); read {
			live = len(sessions)
			top = newestLiveSession(sessions)
		}
	}
	dir, err := agentDir()
	if err != nil {
		return live, top, nil
	}
	recent := events.NewLog(dir).Read(id, 1)
	if len(recent) == 0 {
		return live, top, nil
	}
	e := recent[0]
	return live, top, &launchLastEvent{Kind: e.Kind, Cause: e.Cause, Reason: e.Reason, At: e.At.UnixMilli()}
}

func newestLiveSession(sessions []claudeSession) *launchTopSession {
	for _, sess := range sessions {
		if sess.URL == "" {
			continue
		}
		return &launchTopSession{
			Name:       sessionDisplayName(sess),
			NameSource: sess.NameSource,
			NameSince:  sess.NameSince,
			WaitingFor: sessionWaitingFor(sess),
			Where:      sessionWhereLabel(sess),
			StartedAt:  sess.StartedAt,
			URL:        sess.URL,
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	return &launchTopSession{
		Name:       sessionDisplayName(sessions[0]),
		NameSource: sessions[0].NameSource,
		NameSince:  sessions[0].NameSince,
		WaitingFor: sessionWaitingFor(sessions[0]),
		Where:      sessionWhereLabel(sessions[0]),
		StartedAt:  sessions[0].StartedAt,
	}
}

func sessionDisplayName(sess claudeSession) string {
	if sess.Name != "" {
		return sess.Name
	}
	return "session"
}

// sessionWaitingFor reports what a session is blocked on, in Claude Code's own
// words, and only when it actually said so: "waiting" with nothing named, or a
// session that never writes status at all, must not read as an answer.
func sessionWaitingFor(sess claudeSession) string {
	if w := strings.TrimSpace(sess.WaitingFor); w != "" {
		return w
	}
	if strings.EqualFold(sess.Status, "waiting") || strings.EqualFold(sess.Tempo, "blocked") {
		return "your answer"
	}
	return ""
}

func sessionWhereLabel(sess claudeSession) string {
	entrypoint := strings.ToLower(sess.Entrypoint)
	switch {
	case strings.Contains(entrypoint, "vscode"):
		return "vscode"
	case entrypoint == "sdk-cli":
		return "remote"
	case sess.Kind == "interactive":
		return "terminal"
	case sess.Kind != "":
		return sess.Kind
	}
	return "session"
}

// checkoutTTL bounds how often the branch probe runs. `git status` on a big
// repo is not free and this list is polled every second while a session
// starts, so the request path serves the last answer and refreshes behind it —
// the same deal as the usage sums below.
const checkoutTTL = 20 * time.Second

var checkoutCache = struct {
	mu    sync.Mutex
	repos map[string]*cachedCheckout
}{repos: map[string]*cachedCheckout{}}

type cachedCheckout struct {
	branch     string
	dirty      bool
	computed   time.Time
	refreshing bool
}

func workspaceCheckout(id, absPath string) (string, bool) {
	if absPath == "" {
		return "", false
	}
	checkoutCache.mu.Lock()
	defer checkoutCache.mu.Unlock()
	entry := checkoutCache.repos[id]
	if entry == nil {
		entry = &cachedCheckout{}
		checkoutCache.repos[id] = entry
	}
	fresh := !entry.computed.IsZero() && time.Since(entry.computed) < checkoutTTL
	if !fresh && !entry.refreshing {
		entry.refreshing = true
		go refreshCheckout(id, absPath)
	}
	return entry.branch, entry.dirty
}

func refreshCheckout(id, absPath string) {
	state, ok := utils.ProbeRepoState(absPath)
	checkoutCache.mu.Lock()
	defer checkoutCache.mu.Unlock()
	entry := checkoutCache.repos[id]
	if entry == nil {
		return
	}
	entry.refreshing = false
	entry.computed = time.Now()
	if !ok {
		entry.branch, entry.dirty = "", false
		return
	}
	entry.branch, entry.dirty = state.Branch, state.Dirty
}

// usageTTL is how long a summed report is reused. Summing a workspace means
// scanning its transcripts — a fifth of a second on a busy one — and this list
// is polled every second while a session starts, so the request path must
// never pay for it.
const usageTTL = time.Minute

var usageCache = struct {
	mu   sync.Mutex
	reps map[string]*cachedUsage
}{reps: map[string]*cachedUsage{}}

type cachedUsage struct {
	report     *usage.Report
	computed   time.Time
	refreshing bool
}

// Zero across both windows is reported as nothing, so an idle workspace shows
// no number rather than a row of noughts. A stale report is served while a
// fresh one is summed in the background.
func workspaceUsage(id, absPath, profile string) *usage.Report {
	if absPath == "" {
		return nil
	}
	_, configDir, ok := workspaceSessionTarget(id, profile)
	if !ok {
		return nil
	}

	// Keyed by account too: the same workspace under a different profile has a
	// different transcript directory, and a stale entry would report the other
	// account's tokens.
	key := id + "\x00" + profile
	usageCache.mu.Lock()
	entry := usageCache.reps[key]
	if entry == nil {
		entry = &cachedUsage{}
		usageCache.reps[key] = entry
	}
	fresh := !entry.computed.IsZero() && time.Since(entry.computed) < usageTTL
	report := entry.report
	if !fresh && !entry.refreshing {
		entry.refreshing = true
		go refreshUsage(key, absPath, expandTilde(configDir))
	}
	usageCache.mu.Unlock()
	return report
}

func refreshUsage(key, absPath, configDir string) {
	rep := usage.ForDir(absPath, configDir, mungeClaudeProjectDir(absPath), time.Now())
	usageCache.mu.Lock()
	defer usageCache.mu.Unlock()
	entry := usageCache.reps[key]
	if entry == nil {
		return
	}
	entry.refreshing = false
	entry.computed = time.Now()
	entry.report = nil
	if rep.Week.Total() > 0 {
		copy := rep
		entry.report = &copy
	}
}

// Only the profile NAMES cross the wire; what each selects stays in the
// trusted config.
func launchProfileNames() []string {
	dir, err := agentDir()
	if err != nil {
		return nil
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil || len(user.Profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(user.Profiles))
	for n := range user.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func launchStartHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {workspace, profile} to start")
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
		Profile   string `json:"profile"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the start request")
		return
	}
	if strings.TrimSpace(req.Workspace) == "" {
		writeLaunchError(w, http.StatusBadRequest, "a workspace is required")
		return
	}
	// Same code path as the MCP tool, so the two surfaces cannot drift: it
	// resolves the name, refuses a sensitive workspace, and enqueues the start.
	result, err := mcpSessionStart(req.Workspace, req.Profile, req.Name)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, result)
}

func launchStopHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {workspace} to stop")
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the stop request")
		return
	}
	if strings.TrimSpace(req.Workspace) == "" {
		writeLaunchError(w, http.StatusBadRequest, "a workspace is required")
		return
	}
	result, err := mcpSessionStop(req.Workspace)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, result)
}

// claudeSession is one running Claude Code process for a workspace: the
// per-pid records under <configDir>/sessions, with `claude agents --json` as
// the fallback. BridgeSessionID is the claude.ai id the process registered —
// the one the site resolves — so a terminal or VS Code session gets a real
// link, unlike its local SessionID, which the web does not know.
type claudeSession struct {
	Name string `json:"name"`
	// Written by Claude Code beside the name; see launchTopSession.
	NameSource string `json:"nameSource,omitempty"`
	NameSince  int64  `json:"nameSince,omitempty"`
	// ProcStart is the process's start time as the OS reports it. Claude Code
	// records it so a recycled pid cannot pass for the process that wrote the
	// record — see sessionProcessIsLive.
	ProcStart string `json:"procStart,omitempty"`
	// Status, WaitingFor and Tempo are Claude Code's own live view of the
	// session. Not every session writes them (an interactive one on a laptop
	// often does not), so everything reading them treats absent as unknown
	// rather than idle.
	Status          string `json:"status,omitempty"`
	WaitingFor      string `json:"waitingFor,omitempty"`
	Tempo           string `json:"tempo,omitempty"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Kind            string `json:"kind"`
	Entrypoint      string `json:"entrypoint,omitempty"`
	StartedAt       int64  `json:"startedAt"`
	PID             int    `json:"pid"`
	BridgeSessionID string `json:"bridgeSessionId,omitempty"`
	URL             string `json:"url,omitempty"`
}

// launchSessionsHandler lists the Claude sessions for one workspace. corgi holds
// no Claude credentials of its own: it shells out to `claude agents --json`,
// which reads the local session state the CLI already keeps, scoped to the
// workspace directory and run under that workspace's own account.
func launchSessionsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("workspace"))
	if id == "" {
		writeLaunchError(w, http.StatusBadRequest, "a workspace is required")
		return
	}
	absPath, configDir, ok := workspaceSessionTarget(id, runningProfile(id))
	if !ok {
		writeLaunchError(w, http.StatusNotFound, "unknown workspace")
		return
	}
	writeLaunchJSON(w, map[string]any{
		"sessions": listClaudeSessions(absPath, configDir),
		"links":    bridgeSessionLinks(absPath, configDir),
		"history":  sessionHistory(id),
		"events":   recentEvents(id, 6),
	})
}

func recentEvents(workspaceID string, limit int) []launchLastEvent {
	dir, err := agentDir()
	if err != nil {
		return []launchLastEvent{}
	}
	out := []launchLastEvent{}
	for _, e := range events.NewLog(dir).Read(workspaceID, limit) {
		out = append(out, launchLastEvent{Kind: e.Kind, Cause: e.Cause, Reason: e.Reason, At: e.At.UnixMilli()})
	}
	return out
}

func launchInfoHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	host, _ := os.Hostname()
	info := map[string]any{
		"host":    host,
		"version": APP_VERSION,
		"daemon":  false,
	}
	if dir, err := agentDir(); err == nil {
		if d, derr := daemon.ReadInfo(dir); derr == nil && d != nil {
			info["daemon"] = true
			info["daemonPid"] = d.PID
		}
	}
	if latest := cachedLatestVersion(); latest != "" && latest != APP_VERSION {
		info["latest"] = latest
	}
	writeLaunchJSON(w, info)
}

// launchBoardHandler is the session board for the phone: which Claude
// sessions on this machine are waiting on a person, which are working. The
// same sessions.json the Stream Deck reads, minus nothing — the page decides
// what to show.
func launchBoardHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rep, err := readBoard(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, rep)
}

var latestVersion struct {
	mu      sync.Mutex
	value   string
	checked time.Time
}

// Refreshed off the request path: the page must never wait on GitHub.
func cachedLatestVersion() string {
	latestVersion.mu.Lock()
	defer latestVersion.mu.Unlock()
	if time.Since(latestVersion.checked) < time.Hour {
		return latestVersion.value
	}
	latestVersion.checked = time.Now()
	go func() {
		tag, err := getLatestGitHubTag()
		if err != nil {
			return
		}
		latestVersion.mu.Lock()
		latestVersion.value = strings.TrimPrefix(strings.TrimSpace(tag), "v")
		latestVersion.mu.Unlock()
	}()
	return latestVersion.value
}

// A device may not revoke itself: locking the only paired phone out, from that
// phone, is never what was meant.
func launchDevicesHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not resolve the agent data directory")
		return
	}
	path := pairing.StorePath(dir)
	me, _ := authorizedDevice(path, r.Header.Get("Authorization"))

	switch r.Method {
	case http.MethodGet:
		store, err := pairing.Load(path)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not read the paired devices")
			return
		}
		list := make([]map[string]any, 0, len(store.Devices))
		for _, d := range store.Devices {
			list = append(list, map[string]any{
				"name": d.Name, "pairedAt": d.CreatedAt.UnixMilli(), "current": d.Name == me,
			})
		}
		writeLaunchJSON(w, map[string]any{"devices": list})
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeLaunchError(w, http.StatusBadRequest, "a device name is required")
			return
		}
		if name == me {
			writeLaunchError(w, http.StatusBadRequest, "this device cannot revoke itself — do it from the laptop with `corgi mcp devices revoke`")
			return
		}
		store, err := pairing.Load(path)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not read the paired devices")
			return
		}
		if !store.Revoke(name) {
			writeLaunchError(w, http.StatusNotFound, "no device named "+name)
			return
		}
		if err := pairing.Save(path, store); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not save the paired devices")
			return
		}
		writeLaunchJSON(w, map[string]any{"revoked": name})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET to list, DELETE to revoke")
	}
}

func launchDoctorHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeLaunchJSON(w, map[string]any{"checks": collectAgentChecks()})
}

type launchHistoryEntry struct {
	URL string `json:"url"`
	At  int64  `json:"at"`
}

const (
	sessionHistoryMax = 8
	sessionHistoryAge = 14 * 24 * time.Hour
)

func sessionHistory(workspaceID string) []launchHistoryEntry {
	dir, err := agentDir()
	if err != nil {
		return []launchHistoryEntry{}
	}
	cutoff := time.Now().Add(-sessionHistoryAge)
	seen := map[string]bool{}
	out := []launchHistoryEntry{}
	for _, e := range events.NewLog(dir).Read(workspaceID, 0) {
		if e.Kind != "session" || e.URL == "" || seen[e.URL] || e.At.Before(cutoff) {
			continue
		}
		seen[e.URL] = true
		out = append(out, launchHistoryEntry{URL: e.URL, At: e.At.UnixMilli()})
		if len(out) >= sessionHistoryMax {
			break
		}
	}
	return out
}

// workspaceSessionTarget resolves a workspace id to its directory and the Claude
// config dir (account) its sessions live under. The id is the trusted registry
// key, and the directory comes from the registry — never from the caller — so a
// device token cannot point the shell-out at an arbitrary path.
// workspaceSessionTarget resolves where a workspace's Claude state lives.
//
// profile matters: a workspace running under one keeps its session records and
// its transcripts in that account's config dir, so resolving the default one
// showed the phone no sessions and no token counts for exactly the workspaces
// that run under a second account. Pass "" for the default account.
func workspaceSessionTarget(id, profile string) (absPath, configDir string, ok bool) {
	registry, _, err := agentRegistry()
	if err != nil {
		return "", "", false
	}
	ws, found := registry.Find(id)
	if !found || ws.AbsPath == "" {
		return "", "", false
	}
	dir, err := agentDir()
	if err != nil {
		return ws.AbsPath, "", true
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return ws.AbsPath, "", true
	}
	repo, _ := config.LoadRepo(ws.AbsPath)
	resolved := config.Resolve(id, repo, user)
	if p := strings.TrimSpace(profile); p != "" {
		// An unknown profile is not this function's error to report — the start
		// that named it already refused — so fall back to the default account.
		if withProfile, perr := config.ApplyProfile(resolved, user, p); perr == nil {
			resolved = withProfile
		}
	}
	return ws.AbsPath, resolved.ConfigDir, true
}

// runningProfile is the account the daemon actually started this workspace
// under, for the handlers that are given an id and nothing else.
func runningProfile(id string) string {
	dir, err := agentDir()
	if err != nil {
		return ""
	}
	status, err := daemon.ReadStatus(dir)
	if err != nil || status == nil {
		return ""
	}
	for _, ws := range status.Workspaces {
		if ws.WorkspaceID == id {
			return ws.Profile
		}
	}
	return ""
}

// bridgeSessionLinks reads the claude.ai session URL a remote-control bridge
// recorded for absPath under the account's config dir. This covers sessions
// corgi did NOT start: any `claude remote-control` (or /remote-control in a
// chat) writes projects/<munged-dir>/bridge-pointer.json with the real
// session_… web id — the id namespace claude.ai actually resolves. Best-effort:
// missing or stale (dead pid) pointers yield nothing.
func bridgeSessionLinks(absPath, configDir string) []string {
	base := expandTilde(configDir)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		base = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(base, "projects", mungeClaudeProjectDir(absPath), "bridge-pointer.json"))
	if err != nil {
		return nil
	}
	var bp struct {
		SessionID string `json:"sessionId"`
		PID       int    `json:"pid"`
	}
	if json.Unmarshal(data, &bp) != nil || !strings.HasPrefix(bp.SessionID, "session_") {
		return nil
	}
	if bp.PID > 0 && !pidExists(bp.PID) {
		return nil // the bridge is gone; its pointer is stale
	}
	return []string{"https://claude.ai/code/" + bp.SessionID}
}

// mungeClaudeProjectDir maps a directory to Claude's per-project state folder
// name (its convention: every / and . becomes -).
func mungeClaudeProjectDir(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-", "\\", "-", ":", "-").Replace(dir)
}

// pidExists is a plain liveness probe. Not PidAlive: a hand-started bridge runs
// under a shell and is no process-group leader, which PidAlive requires.
// sessionProcessIsLive answers whether the process that wrote a session record
// is still the process running under that pid.
//
// A live pid is not enough on its own: session records outlive the machine
// (they sit in the config dir across reboots) and pids are handed out again
// from low numbers after one, so a stale record whose pid now belongs to
// something else made a finished session show up as live — a phantom "1 live"
// on a workspace with nothing running in it. Claude Code writes procStart for
// exactly this check, so where the OS will tell us a process's start time
// cheaply, compare it. Where it will not, a live pid stays the best answer
// available, which is what this did everywhere before.
func sessionProcessIsLive(cs claudeSession) bool {
	if !pidExists(cs.PID) {
		return false
	}
	started, ok := processStartToken(cs.PID)
	if !ok || strings.TrimSpace(cs.ProcStart) == "" {
		return true
	}
	return sameStartToken(cs.ProcStart, started)
}

// processStartToken reads the start time the OS keeps for a pid, in the exact
// form Claude Code records it, so the two can be compared as strings:
//
//	linux   field 22 of /proc/<pid>/stat — clock ticks since boot
//	darwin  the output of `ps -o lstart= -p <pid>` under LC_ALL=C and TZ=UTC
//
// ok is false where neither is available (Windows, or the read failed), and
// the caller then falls back to "a live pid is live", as it did everywhere
// before this existed. Comparing a token we could not read is never worth a
// session vanishing from the list.
func processStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	if token, ok := cachedStartToken(pid); ok {
		return token, true
	}
	var token string
	var ok bool
	switch runtime.GOOS {
	case "linux":
		token, ok = linuxStartTicks(pid)
	case "darwin":
		token, ok = darwinStartDate(pid)
	}
	if ok {
		rememberStartToken(pid, token)
	}
	return token, ok
}

// startTokenTTL bounds how long a read is reused. A process's start time never
// changes, so this only exists so a recycled pid cannot keep a dead session's
// answer — and so the darwin path costs at most one `ps` per pid per minute
// while the phone polls this list every second.
const startTokenTTL = time.Minute

var startTokens = struct {
	mu   sync.Mutex
	seen map[int]startToken
}{seen: map[int]startToken{}}

type startToken struct {
	value string
	read  time.Time
}

func cachedStartToken(pid int) (string, bool) {
	startTokens.mu.Lock()
	defer startTokens.mu.Unlock()
	entry, ok := startTokens.seen[pid]
	if !ok || time.Since(entry.read) > startTokenTTL {
		return "", false
	}
	return entry.value, true
}

func rememberStartToken(pid int, value string) {
	startTokens.mu.Lock()
	defer startTokens.mu.Unlock()
	if len(startTokens.seen) > 512 {
		// A daemon alive for weeks must not accumulate an entry per pid ever
		// seen; every entry is re-readable, so dropping them costs one read.
		startTokens.seen = map[int]startToken{}
	}
	startTokens.seen[pid] = startToken{value: value, read: time.Now()}
}

func linuxStartTicks(pid int) (string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	// The second field is the executable name in parentheses and may itself
	// contain spaces and parentheses, so fields are counted from the last ')'.
	closing := strings.LastIndex(string(data), ")")
	if closing < 0 {
		return "", false
	}
	fields := strings.Fields(string(data)[closing+1:])
	// state is the field after comm, so starttime (22nd overall) is index 19
	// of what is left.
	const startTimeOffset = 19
	if len(fields) <= startTimeOffset {
		return "", false
	}
	return fields[startTimeOffset], true
}

// darwinStartDate runs the same command Claude Code does, with the same
// locale and timezone pinned, so the strings line up. Bounded: this sits on a
// request path the phone polls.
func darwinStartDate(pid int) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)) // NOSONAR — fixed argv, numeric pid
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", false
	}
	return line, true
}

// sameStartToken compares two start times for the same pid. Internal spacing
// is normalised first: `ps` pads a single-digit day ("Sep  3"), and a session
// disappearing from the phone over a space would be a far worse bug than the
// stale record this is here to catch.
func sameStartToken(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

func pidExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		_ = proc.Release()
		return true
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// listClaudeSessions returns the Claude sessions under absPath. Best-effort: a
// missing claude binary, a timeout, or no sessions all resolve to an empty list,
// so the launcher shows "no sessions" rather than a 500.
func listClaudeSessions(absPath, configDir string) []claudeSession {
	if sessions, ok := localClaudeSessions(absPath, configDir); ok {
		return sessions
	}
	return claudeAgentsSessions(absPath, configDir)
}

// localClaudeSessions reads the per-process records Claude Code keeps under
// <configDir>/sessions/<pid>.json. ok is false when the directory is absent
// (an older CLI), so the caller can fall back to shelling out.
func localClaudeSessions(absPath, configDir string) ([]claudeSession, bool) {
	base := expandTilde(configDir)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, false
		}
		base = filepath.Join(home, ".claude")
	}
	entries, err := os.ReadDir(filepath.Join(base, "sessions"))
	if err != nil {
		return nil, false
	}
	out := []claudeSession{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(base, "sessions", e.Name()))
		if err != nil {
			continue
		}
		var cs claudeSession
		if json.Unmarshal(data, &cs) != nil || cs.CWD == "" {
			continue
		}
		if cs.CWD != absPath && !strings.HasPrefix(cs.CWD, absPath+string(filepath.Separator)) {
			continue
		}
		if !sessionProcessIsLive(cs) {
			continue
		}
		if strings.HasPrefix(cs.BridgeSessionID, "session_") {
			cs.URL = "https://claude.ai/code/" + cs.BridgeSessionID
		}
		out = append(out, cs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out, true
}

func claudeAgentsSessions(absPath, configDir string) []claudeSession {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// Fixed argv (no shell); absPath is the trusted registry path.
	cmd := exec.CommandContext(ctx, "claude", "agents", "--json", "--cwd", absPath)
	cmd.Env = os.Environ()
	if d := expandTilde(configDir); d != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+d)
	}
	out, err := cmd.Output()
	if err != nil {
		return []claudeSession{}
	}
	var sessions []claudeSession
	if json.Unmarshal(out, &sessions) != nil {
		return []claudeSession{}
	}
	return sessions
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func launcherPageHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, launcherPageHTML)
}

func setLaunchHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeLaunchJSON(w http.ResponseWriter, v any) {
	w.Header().Set(headerContentType, mimeJSON)
	_ = json.NewEncoder(w).Encode(v)
}

func writeLaunchError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set(headerContentType, mimeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// launcherPageHTML is the phone launcher. Self-contained (no external assets),
// reads the device token from localStorage, and talks only to same-origin
// /launch/* endpoints. Every dynamic value is escaped before it reaches the DOM.
const launcherPageHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#0b0d12">
<title>corgi</title>
<style>
  /* One palette, read by everything below: dark like the trackers this page
     sits next to, one accent, and lines you notice only when you look. */
  :root{--bg:#08090a;--card:#101113;--card2:#16181b;--line:#212327;--hair:#1a1c1f;
      --text:#eceef1;--fg:#eceef1;--dim:#8a8f98;--dim2:#5c6169;
      --accent:#5e6ad2;--accent-soft:#171a2e;--accent-line:#33376b;
      --green:#3fb950;--amber:#d29922;--red:#f85149;
      --sp1:.25rem;--sp2:.5rem;--sp3:.75rem;--sp4:1rem;--r:.5rem}
  *{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
  /* Flex and grid beat the hidden attribute on specificity, and this page
     toggles controls with .hidden. */
  [hidden]{display:none!important}
  html{-webkit-text-size-adjust:100%}
  /* Once for the page: a per-element list meant every row added since then
     double-tap-zoomed instead of registering the second tap. */
  html{touch-action:manipulation}
  button,a,summary,label,input,select,textarea{touch-action:manipulation}
  body{font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;
      padding-bottom:env(safe-area-inset-bottom);-webkit-font-smoothing:antialiased;
      overscroll-behavior-y:none}
  header{position:sticky;top:0;z-index:30;background:rgba(8,9,10,.88);backdrop-filter:blur(14px);
      -webkit-backdrop-filter:blur(14px);border-bottom:1px solid var(--line);
      padding-top:calc(.7rem + env(safe-area-inset-top));padding-bottom:0;
      padding-left:max(1.2rem,env(safe-area-inset-left));padding-right:max(1.2rem,env(safe-area-inset-right))}
  header>*{max-width:34rem;margin-left:auto;margin-right:auto}
  .brand{display:flex;align-items:center;gap:.7rem}
  .brand .who{min-width:0;flex:1}
  .chip.refresh{flex:0 0 auto;width:2rem;height:2rem;padding:0;display:flex;align-items:center;
      justify-content:center;font-size:.85rem;color:var(--dim)}
  .chip.refresh:active{color:var(--text)}
  .chip.refresh i{display:block;font-style:normal;line-height:1}
  .chip.refresh.spin i{animation:spin .7s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .logo{width:1.9rem;height:1.9rem;border-radius:.6rem;background:var(--card2);
      border:1px solid var(--line);display:flex;align-items:center;justify-content:center;font-size:1rem;flex:0 0 auto}
  h1{font-size:1rem;margin:0;letter-spacing:-.01em;font-weight:600}
  header small{display:block;color:var(--dim);font-size:.78rem;font-weight:400;margin-top:.1rem}
  header small .what{color:var(--dim);border-bottom:1px dotted var(--line-soft,#3a4152);cursor:pointer}
  .hostnote{color:var(--dim);font-size:.74rem;line-height:1.5;margin:.45rem 0 0;max-width:30rem}
  /* The last row has to clear Safari's floating toolbar, which sits over the
     page rather than beside it, and the home indicator under that. */
  main{max-width:34rem;margin:0 auto;padding-top:.7rem;
      padding-bottom:calc(5rem + env(safe-area-inset-bottom));
      padding-left:max(1.2rem,env(safe-area-inset-left));
      padding-right:max(1.2rem,env(safe-area-inset-right))}

  /* Tabs. The page answers three questions and they are not equally urgent:
     what wants me, what is running, what could run. One at a time. */
  .tabs{display:flex;gap:.15rem;padding:.5rem .6rem .45rem 0;overflow-x:auto;scrollbar-width:none;
      -webkit-overflow-scrolling:touch;scroll-snap-type:x proximity}
  .tabs button{scroll-snap-align:start}
  .tabs::-webkit-scrollbar{display:none}
  .tabs button{flex:0 0 auto}
  .tabs button{padding:.35rem .6rem;border-radius:.4rem;border:0;background:none;
      color:var(--dim);font-size:.82rem;font-weight:500}
  .tabs button[aria-selected=true]{color:var(--text);background:var(--card2)}
  .tabs .n{margin-left:.35rem;font-size:.68rem;color:var(--dim2);background:#1e2024;
      border-radius:1rem;padding:.05rem .35rem}
  .tabs button[aria-selected=true] .n{color:var(--text)}
  .tabs .n:empty{display:none}

  /* A sheet, because twelve tracker columns do not fit in a row of buttons
     on a phone and a dropdown on iOS is worse than either. */
  .scrim{position:fixed;inset:0;background:rgba(0,0,0,.6);z-index:60;display:flex;align-items:flex-end}
  .sheet{width:100%;background:var(--card);border-top-left-radius:.9rem;border-top-right-radius:.9rem;
      border-top:1px solid var(--line);max-height:76vh;overflow:auto;
      padding:.5rem 0 calc(.6rem + env(safe-area-inset-bottom))}
  .grab{width:2.2rem;height:.25rem;border-radius:.2rem;background:#2a2d33;margin:.35rem auto .6rem}
  .sheet h3{font-size:.68rem;letter-spacing:.07em;text-transform:uppercase;color:var(--dim2);
      margin:.4rem 1rem .4rem;font-weight:600}
  .opt{display:block;width:100%;text-align:left;padding:.8rem 1rem;background:none;border:0;
      border-bottom:1px solid var(--hair);color:var(--text);font:inherit;font-size:.88rem}
  .opt:last-child{border-bottom:0}
  .opt:disabled{color:var(--dim2)}
  .opt.here{color:var(--accent);font-weight:600}
  .gcount{margin-left:auto;font-size:.66rem;color:var(--accent);font-weight:600}
  /* The batch bar: only there once something is selected. */
  .batch{display:flex;flex-wrap:wrap;align-items:center;gap:.4rem;margin:.2rem 0 .4rem;
    padding:.45rem .55rem;background:var(--card2);border:1px solid var(--line);border-radius:.6rem}
  .batch .bcount{font-size:.74rem;color:var(--dim);margin-right:.2rem}
  .batch button{font:inherit;font-size:.76rem;font-weight:500;padding:.4rem .7rem;border-radius:.45rem;
    border:1px solid var(--line);background:transparent;color:var(--dim);min-height:2rem}
  .batch button.primary{background:var(--accent);border-color:var(--accent);color:#fff}
  .batch button:disabled{opacity:.5}
  .linkme{margin-left:.45rem;font:inherit;font-size:.68rem;padding:.1rem .45rem;border-radius:.4rem;
    border:1px solid var(--line);background:var(--card2);color:var(--dim)}
  .linkme:disabled{opacity:.6}
  /* The column the ticket sits in, on the row that offers to change it. */
  .ev .estate{margin-left:auto;font-size:.64rem;letter-spacing:.04em;text-transform:uppercase;
    color:var(--dim);border:1px solid var(--line);border-radius:.5rem;padding:.1rem .4rem;white-space:nowrap}

  /* The session board: what every Claude on the machine is doing, the ones
     waiting on a person first. Hidden until tracking reports anything. */
  .board{margin:var(--sp2) 0 var(--sp3)}
  .board .sum{color:var(--dim);font-size:.74rem;margin:0 0 .35rem .1rem}
  .board .sum.hot{color:var(--red);font-weight:600}
  .sess{display:flex;align-items:center;gap:.6rem;background:var(--card2);border:1px solid var(--hair);
      border-radius:.6rem;padding:.5rem .7rem;margin:.3rem 0;font-size:.82rem}
  .sess .sdot{margin:0}
  .sess .sdot.needs{background:var(--red);animation:pulse 1s ease-in-out infinite}
  .sess .sdot.working{background:var(--amber)}
  .sess .sdot.done{background:var(--green);opacity:.6}
  .sess .sdot.stale,.sess .sdot.gone,.sess .sdot.unknown{background:var(--dim2)}
  .sess .sdot.limited{background:#5B8DEF}
  .acct{display:flex;flex-wrap:wrap;gap:.35rem .8rem;font-size:.74rem;color:var(--dim);margin:0 0 .45rem .1rem}
  .acct b{color:var(--fg);font-weight:600}
  .acct .bar{display:inline-block;width:3.2rem;height:.4rem;border-radius:.2rem;background:var(--hair);vertical-align:middle;margin:0 .3rem;overflow:hidden}
  .acct .bar i{display:block;height:100%;background:var(--green)}
  .acct .bar.warm i{background:var(--amber)}
  .acct .bar.hot i{background:var(--red)}
  .acct .out{color:var(--red);font-weight:600}
  .sess .slabel{font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:1}
  .sess .sdetail{color:var(--dim);font-size:.72rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:45%}
  .sess .sbadge{font-size:.6rem;font-weight:700;color:var(--dim);border:1px solid var(--line);border-radius:.3rem;
      padding:.05rem .3rem;flex:0 0 auto;text-transform:uppercase;letter-spacing:.04em}
  .newchat{background:var(--card2);border:1px solid var(--hair);border-radius:.6rem;padding:.55rem .7rem;margin:.3rem 0 .5rem}
  .newchat textarea{width:100%;box-sizing:border-box;font:inherit;font-size:.82rem;padding:.45rem .55rem;border-radius:.45rem;
      border:1px solid var(--line);background:var(--card);color:var(--fg);resize:vertical;min-height:2.6rem}
  /* Four pickers do not fit across a phone; let them wrap and keep the
     button on its own end so it never ends up half a word wide. */
  .newchat .nrow{display:flex;flex-wrap:wrap;gap:.4rem;margin-top:.4rem;align-items:center}
  .newchat .nrow select{flex:1 1 7rem;min-width:6.5rem;max-width:none}
  .newchat .nrow button{margin-left:auto}
  .newchat select{font:inherit;font-size:.74rem;flex:1;min-width:0;padding:.3rem .4rem;border-radius:.45rem;border:1px solid var(--line);
      background:var(--card);color:var(--fg)}
  .newchat button{font:inherit;font-size:.76rem;font-weight:500;padding:.34rem .8rem;border-radius:.45rem;border:1px solid var(--accent);
      background:var(--accent);color:#fff;cursor:pointer;flex:0 0 auto}
  .newchat button:disabled{opacity:.5}
  .runlog{font:.68rem/1.35 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere;max-height:60vh;overflow:auto;background:var(--card2);border:1px solid var(--hair);border-radius:.5rem;padding:.5rem;margin:.4rem 0 0}
  .kb{display:flex;gap:.6rem;overflow-x:auto;padding-bottom:.5rem;scroll-snap-type:x mandatory}
  .kb .kcol{flex:0 0 78vw;max-width:340px;scroll-snap-align:start;background:var(--card);border:1px solid var(--hair);border-radius:.7rem;padding:.5rem}
  .kb .kcol h3{margin:0 0 .4rem;font-size:.8rem;letter-spacing:.06em;text-transform:uppercase;opacity:.75;display:flex;justify-content:space-between}
  .kb .kcol h3 span{opacity:.7}
  .kb .kcard{background:var(--card2);border:1px solid var(--hair);border-radius:.55rem;padding:.45rem .55rem;margin:.3rem 0}
  .kb .kcard .kref{font-weight:600;font-size:.85rem}
  .kb .kcard .kws{font-size:.7rem;opacity:.6;margin-left:.4rem}
  .kb .kcard .kwhy{font-size:.75rem;opacity:.85;margin:.15rem 0}
  .kb .kcard .ktitle{font-size:.78rem;opacity:.7;margin:0 0 .3rem;overflow-wrap:anywhere}
  .kb .kcard .kmeta{font-size:.7rem;opacity:.7;margin:.1rem 0}
  .kb .kcard.blocked{border-color:#E4695B}
  .kb .kcard.running{border-color:#5B8DEF}
  .kb .empty{font-size:.75rem;opacity:.5;padding:.4rem .2rem}
  .ev{background:var(--card2);border:1px solid var(--hair);border-radius:.6rem;padding:.5rem .65rem;margin:.3rem 0}
  .ev .eref{font-weight:600;font-size:.82rem}
  .ev .ekind{font-size:.66rem;text-transform:uppercase;letter-spacing:.04em;opacity:.6;margin-left:.35rem}
  .ev .etitle{font-size:.78rem;opacity:.85;margin:.15rem 0 .4rem;overflow-wrap:anywhere}
  .eblocked{margin:6px 0 0;font-size:13px;color:#E4695B}
  .ev .erow{display:flex;gap:.4rem;align-items:center}
  .ev a.eopen{font-size:.76rem;text-decoration:none;padding:.45rem .75rem;border-radius:.45rem;
    border:1px solid var(--line);color:inherit;min-height:2rem;display:inline-flex;align-items:center}
  .ev button{font:inherit;font-size:.76rem;font-weight:500;padding:.45rem .75rem;border-radius:.45rem;min-height:2rem;
    border:1px solid var(--line);background:transparent;color:var(--dim)}
  .ev button:disabled{opacity:.5}
  /* One action leads; the rest are there when you want them. Four equally
     loud buttons on a row is the same as none. */
  .ev .erow button.primary{border-color:var(--accent);background:var(--accent);color:#fff;font-weight:500}
  .ev .erow{flex-wrap:wrap}
  .evgroup{margin:.5rem 0 .2rem;display:flex;align-items:center;gap:.5rem}
  .evgroup .gname{font-size:.74rem;font-weight:600;text-transform:uppercase;letter-spacing:.04em;opacity:.65}
  .evgroup button{font:inherit;font-size:.74rem;font-weight:600;padding:.26rem .6rem;border-radius:.45rem;
    border:1px solid var(--accent);background:var(--accent);color:#fff;margin-left:auto}
  .evgroup button[hidden]{display:none}
  .ev input[type=checkbox]{width:1rem;height:1rem;accent-color:var(--accent);margin-right:.1rem}
  .sess{flex-wrap:wrap}
  .sess .ssum{flex-basis:100%;color:var(--dim);font-size:.72rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-top:.1rem}
  .sess .sact{flex-basis:100%;display:flex;gap:.4rem;align-items:center;margin-top:.35rem}
  .sess .sact button{font:inherit;font-size:.74rem;font-weight:600;padding:.28rem .7rem;border-radius:.45rem;border:1px solid var(--line);
      background:var(--card);color:var(--fg);cursor:pointer}
  .sess .sact button.ok{background:var(--accent);border-color:var(--accent);color:#fff}
  .sess .sact button.bad{color:var(--red);border-color:var(--red)}
  .sess .sact a{margin-left:auto;font-size:.74rem;color:var(--blue,#5B8DEF)}
  .ws{background:var(--card);border:1px solid var(--line);border-radius:.75rem;
      padding:var(--sp3) var(--sp3) var(--sp2);margin:var(--sp2) 0}
  .head{display:flex;align-items:center;gap:.7rem}
  .main{min-width:0;flex:1}
  .ws .name{font-weight:600;display:flex;align-items:center;gap:.5rem;font-size:.94rem;white-space:nowrap;
      overflow:hidden;text-overflow:ellipsis;letter-spacing:-.006em}
  .ws .path{color:var(--dim2);font-size:.7rem;margin-top:.15rem;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
      white-space:nowrap;overflow:hidden;text-overflow:ellipsis;cursor:pointer}
  .ws .path.full{white-space:normal;word-break:break-all}
  .ws .wnote{color:var(--red);font-size:.72rem;margin-top:.5rem;line-height:1.45}
  /* The dot carries the state: green live, amber blocking on you, red a start
     that was refused, grey nothing running. */
  .dot{width:.55rem;height:.55rem;border-radius:50%;background:#3a4152;flex:0 0 auto}
  .dot.live{background:var(--green)}
  .dot.ready{background:var(--green);opacity:.55}
  .dot.starting{background:var(--green);animation:pulse 1s ease-in-out infinite}
  .dot.attention{background:var(--amber)}
  .dot.blocked{background:var(--red)}
  @keyframes pulse{50%{opacity:.3}}
  .meta{display:flex;align-items:baseline;gap:.4rem;flex-wrap:wrap;margin-top:.25rem;
      font-size:.73rem;color:var(--dim2)}
  .meta span + span::before{content:"\00b7";color:var(--dim2);margin-right:.4rem}
  .meta .live{color:var(--green)}
  .meta .warn{color:var(--red)}
  .meta .why{color:var(--dim)}
  .usage{font-size:.68rem;color:var(--dim2);margin-top:.15rem;
      font-variant-numeric:tabular-nums;opacity:.85}
  .actions{display:flex;align-items:center;gap:.4rem;margin-top:.65rem;flex-wrap:wrap}
  .startbox{flex-basis:100%;display:none;gap:.4rem;margin-top:.5rem}
  .startbox.on{display:flex}
  .startbox select,.startbox input{flex:1 1 6rem;min-width:0;background:var(--card2);border:1px solid var(--line);
      color:var(--text);border-radius:.5rem;padding:.4rem .55rem;font-size:.78rem;font-family:inherit}
  .evrow{display:flex;justify-content:space-between;gap:.6rem;font-size:.72rem;color:var(--dim);padding:.2rem .1rem}
  .evrow b{font-weight:600;color:#c9cfda}
  .dev{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.8rem;
      padding:.4rem 0;border-bottom:1px solid var(--line)}
  .dev:last-child{border-bottom:0}
  .dev .sub{color:var(--dim2);font-size:.7rem}
  .dev button{background:none;border:1px solid rgba(255,123,114,.45);color:var(--red);font-size:.7rem;
      padding:.25rem .6rem;border-radius:.5rem}
  .chk{display:flex;gap:.5rem;font-size:.78rem;padding:.35rem 0;border-bottom:1px solid var(--line);line-height:1.45}
  .chk:last-child{border-bottom:0}
  .chk .fix{color:var(--dim);display:block;font-size:.72rem;margin-top:.15rem}
  .chk.bad .mark{color:var(--red)}
  .chk .mark{color:var(--green);flex:0 0 auto}
  .chip{background:none;border:1px solid var(--line);color:var(--dim);font-size:.72rem;font-weight:500;
      padding:.32rem .6rem;border-radius:var(--r);cursor:pointer}
  .chip b{color:var(--text);font-weight:600}
  .chip.danger{border-color:transparent;color:var(--dim);margin-left:auto}
  .chip.danger:active{color:var(--red)}
  .top{display:flex;align-items:center;justify-content:space-between;gap:.6rem;
      margin-top:var(--sp2);padding:var(--sp2) 0 0;border-top:1px solid var(--hair);
      font-size:.78rem;color:var(--text);text-decoration:none;min-height:2rem}
  .top span:first-child{display:flex;align-items:center;flex:1 1 auto;min-width:3rem;overflow:hidden}
  .tname{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .top .when{color:var(--dim2);font-size:.7rem;flex:0 0 auto;max-width:60%;
      overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .intro{color:var(--dim);font-size:.8rem;line-height:1.5;margin:.2rem 0 .9rem}
  .skel{background:var(--card);border:1px solid var(--line);border-radius:.75rem;height:5.2rem;
      margin:var(--sp2) 0;
      background-image:linear-gradient(100deg,transparent 20%,rgba(255,255,255,.045) 40%,transparent 60%);
      background-size:220% 100%;animation:sweep 1.2s linear infinite}
  @keyframes sweep{to{background-position:-220% 0}}
  .sessions{margin-top:var(--sp2);border-top:1px solid var(--hair);padding-top:var(--sp2)}
  .sessions .s{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.76rem;
      color:#c9cfda;padding:.45rem .1rem;min-height:2.1rem;text-decoration:none}
  .sessions .s span:first-child{display:flex;align-items:center;flex:1 1 auto;min-width:3rem;
      overflow:hidden;color:var(--text)}
  .sdot{width:.375rem;height:.375rem;border-radius:50%;background:var(--green);margin-right:.5rem;flex:0 0 auto}
  a.s.past span:first-child{color:var(--dim)}
  a.s.past .when{color:#5b6274}
  .sessions .grp{display:flex;align-items:center;gap:.5rem;margin:.6rem .1rem .1rem;color:var(--dim2);
      font-size:.62rem;font-weight:600;letter-spacing:.08em;text-transform:uppercase}
  .sessions .grp i{flex:1;height:1px;background:var(--hair)}
  .sessions .s .when{color:var(--dim2);font-size:.68rem;flex:0 0 auto;max-width:60%;
      overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .sessions .none,.sessions .hint{color:var(--dim2);font-size:.7rem;line-height:1.45;padding:.2rem 0}
  .kv{display:flex;gap:.6rem;font-size:.72rem;padding:.22rem .1rem;line-height:1.45}
  .kv b{color:var(--dim2);font-weight:600;flex:0 0 5.4rem}
  .kv span{color:#c9cfda;min-width:0}
  .tag{font-size:.6rem;font-weight:700;color:var(--amber);border:1px solid rgba(255,166,87,.4);
      border-radius:.35rem;padding:.05rem .3rem;margin-left:.45rem;vertical-align:1px;flex:0 0 auto;
      white-space:nowrap}
  button{border:0;border-radius:var(--r);padding:.48rem .9rem;font-size:.82rem;font-weight:600;cursor:pointer;
      background:#1e2027;color:var(--text);transition:transform .05s;flex:0 0 auto}
  button:active{transform:scale(.97)}
  button:disabled{opacity:.5}
  a.open,button.open{display:inline-block;background:var(--green);color:#08110a;text-decoration:none;border:0;
      padding:.48rem .9rem;border-radius:var(--r);font-weight:600;font-size:.82rem;cursor:pointer;flex:0 0 auto}
  .msg{color:var(--dim);font-size:.9rem;margin:1rem 0;line-height:1.5}
  .empty{padding:1.6rem 0 .6rem}
  .empty h2{font-size:1.05rem;margin:0 0 .3rem;letter-spacing:-.01em}
  .empty p{color:var(--dim);font-size:.82rem;line-height:1.6;margin:0}
  .err{color:var(--red)}
  code{background:var(--card);border:1px solid var(--line);padding:.1rem .35rem;border-radius:.35rem;font-size:.85em}
  .tips{margin:1.5rem 0 0;background:var(--card);border:1px solid var(--line);border-radius:.75rem;
      padding:0 var(--sp3)}
  .tips summary{display:flex;align-items:center;justify-content:space-between;gap:var(--sp2);
      list-style:none;cursor:pointer;padding:var(--sp3) 0;
      font-size:.64rem;font-weight:600;letter-spacing:.08em;text-transform:uppercase;color:var(--dim2)}
  .tips summary::-webkit-details-marker{display:none}
  .tips-hint{text-transform:none;letter-spacing:0;font-weight:400;font-size:.72rem;color:var(--dim2)}
  .tips[open] .tips-hint::after{content:" \2303"}
  .tips:not([open]) .tips-hint::after{content:" \2304"}
  .tips > .tip:first-of-type{border-top:1px solid var(--hair);padding-top:var(--sp3)}
  .tips[open]{padding-bottom:var(--sp3)}
  .tip{display:flex;flex-direction:column;align-items:stretch;gap:.15rem;width:100%;text-align:left;
      background:none;border:0;border-top:1px solid var(--hair);border-radius:0;
      padding:var(--sp3) 0 var(--sp2);margin:0;cursor:pointer;font-family:inherit}

  .tip-t{font-size:.84rem;font-weight:600;color:var(--text);letter-spacing:-.005em}
  .tip-d{font-size:.75rem;color:var(--dim);line-height:1.5}
  .tip-cmd{display:flex;align-items:center;justify-content:space-between;gap:var(--sp2);
      margin-top:var(--sp2);background:var(--card2);border-radius:var(--r);padding:.42rem .5rem}
  .tip-cmd code{min-width:0;overflow-x:auto;white-space:nowrap;font-size:.7rem;color:var(--text);
      background:none;border:0;padding:0}
  .tip-copy{font-size:.66rem;font-weight:600;color:var(--dim2);flex:0 0 auto;letter-spacing:.02em}
  .tip.copied .tip-copy{color:var(--green)}
  .tipnote{color:var(--dim2);font-size:.72rem;margin:var(--sp3) 0 0}
  /* In a tab of their own these are the page, not a card folded into the
     bottom of one. The rest of the app is cards; these were hairline rows
     inside one big card, which is what read as a different app. */
  [data-pane="laptop"] details.tips, [data-pane="settings"] details.settings{
      margin:0;background:none;border:0;border-radius:0;padding:0}
  [data-pane="laptop"] details.tips>summary, [data-pane="settings"] details.settings>summary{display:none}
  [data-pane="laptop"] .tip{
      background:var(--card2);border:1px solid var(--hair);border-radius:.6rem;
      padding:.6rem .7rem;margin:.4rem 0;gap:.2rem}
  [data-pane="laptop"] .tips > .tip:first-of-type{border-top:1px solid var(--hair);padding-top:.6rem}
  [data-pane="laptop"] .tipnote{margin:.8rem .1rem 0;color:var(--dim2);font-size:.72rem}
  /* Settings: each block is its own card, headed like a section elsewhere. */
  [data-pane="settings"] details.settings h3{
      margin:1.1rem .1rem .35rem;font-size:.64rem;letter-spacing:.08em;color:var(--dim2)}
  [data-pane="settings"] details.settings h3:first-of-type{margin-top:.2rem}
  [data-pane="settings"] details.settings p,
  [data-pane="settings"] details.settings pre,
  [data-pane="settings"] details.settings .chk,
  [data-pane="settings"] details.settings .toggle,
  [data-pane="settings"] #devices, [data-pane="settings"] #doctor{
      background:var(--card2);border:1px solid var(--hair);border-radius:.6rem;
      padding:.55rem .7rem;margin:.35rem 0}
  [data-pane="settings"] details.settings .chk{border-bottom:0}
  [data-pane="settings"] details.settings button{
      background:var(--card2);border:1px solid var(--line);border-radius:.6rem;
      color:var(--text);padding:.5rem .8rem;margin:.35rem 0;font-size:.8rem}
  details.settings{margin:1.6rem 0 0;background:var(--card2);border:1px solid var(--line);
      border-radius:1rem;padding:.4rem 1rem}
  details.settings h3{font-size:.66rem;font-weight:700;letter-spacing:.1em;text-transform:uppercase;
      color:var(--dim2);margin:1.4rem 0 .4rem}
  details.settings h3:first-of-type{margin-top:.6rem}
  details.settings summary{color:var(--dim);font-size:.85rem;cursor:pointer;padding:.5rem 0;list-style:none}
  details.settings summary::-webkit-details-marker{display:none}
  details.settings p{color:var(--dim);font-size:.76rem;line-height:1.55}
  label.toggle{display:flex;align-items:center;gap:.5rem;color:var(--dim);font-size:.78rem;
      padding:.3rem 0 .7rem;cursor:pointer}
  label.toggle input{accent-color:var(--green);width:1rem;height:1rem;margin:0}
  pre{background:var(--bg);border:1px solid var(--line);border-radius:.6rem;padding:.7rem;
      overflow-x:auto;font-size:.7rem;line-height:1.45;white-space:pre;color:#c9cfda}
  .toast{position:fixed;left:50%;bottom:calc(1rem + env(safe-area-inset-bottom));
      transform:translate(-50%,.6rem);z-index:60;max-width:calc(100% - 2rem);
      background:var(--text);color:#0b0d12;font-size:.78rem;font-weight:600;padding:.5rem .8rem;
      border-radius:var(--r);opacity:0;transition:opacity .16s,transform .16s;
      box-shadow:0 .5rem 1.4rem rgba(0,0,0,.55)}
  .toast.on{opacity:1;transform:translate(-50%,0)}
  .toast.bad{background:var(--red);color:#1a0503}
  @media (prefers-reduced-motion:reduce){.toast,.skel,.dot.starting,.chip.refresh.spin i{animation:none}}
  .foot{text-align:center;margin-top:1.6rem;font-size:.85rem}
  .foot a{color:var(--green);text-decoration:none}
</style>
<header>
  <div class="brand"><span class="logo">🐕</span>
    <div class="who"><h1>corgi</h1><small id="host">your machine</small></div>
    <button class="chip refresh" id="refresh" aria-label="Refresh" title="Refresh"><i>&#x21bb;</i></button>
  </div>
  <p id="hostnote" class="hostnote" hidden></p>
  <div class="tabs" id="tabs" role="tablist">
    <button role="tab" data-tab="inbox" aria-selected="true">Inbox<span class="n" id="n-inbox"></span></button>
    <button role="tab" data-tab="kanban" aria-selected="false">Board<span class="n" id="n-kanban"></span></button>
    <button role="tab" data-tab="sessions" aria-selected="false">Sessions<span class="n" id="n-sessions"></span></button>
    <button role="tab" data-tab="stacks" aria-selected="false">Stacks<span class="n" id="n-stacks"></span></button>
    <button role="tab" data-tab="laptop" aria-selected="false">Laptop</button>
    <button role="tab" data-tab="settings" aria-selected="false">Settings</button>
  </div>
</header>
<main>
  <div data-pane="inbox">
    <section id="inbox" class="board" hidden></section>
    <section id="newchat" class="board" hidden></section>
  </div>
  <div data-pane="kanban" hidden>
    <section id="kanban" class="board" hidden></section>
  </div>
  <div data-pane="sessions" hidden>
    <section id="board" class="board" hidden></section>
  </div>
  <div data-pane="stacks" hidden>
    <div id="list" class="msg">Loading…</div>
  </div>
  <div data-pane="laptop" hidden>
  <details class="tips" id="tips" hidden>
    <summary><span>On the laptop</span><span class="tips-hint">setup commands</span></summary>
    <button class="tip" data-copy="/corgi-remote">
      <span class="tip-t">Set it all up, guided</span>
      <span class="tip-d">With the corgi plugin installed, this skill walks Claude Code through registering the repo, the tunnel and login start.</span>
      <span class="tip-cmd"><code>/corgi-remote</code><span class="tip-copy">COPY</span></span>
    </button>
    <button class="tip" data-copy="corgi agent init">
      <span class="tip-t">Add another repo</span>
      <span class="tip-d">Run it inside any git repo and it joins this list.</span>
      <span class="tip-cmd"><code>cd ~/dev/api &amp;&amp; corgi agent init</code><span class="tip-copy">COPY</span></span>
    </button>
    <button class="tip" data-copy="corgi agent tunnel setup corgi.yourdomain.com">
      <span class="tip-t">Keep this link working</span>
      <span class="tip-d">The free tunnel changes address on every restart, which un-pairs your phone. Point it at a host you own once.</span>
      <span class="tip-cmd"><code>corgi agent tunnel setup corgi.yourdomain.com</code><span class="tip-copy">COPY</span></span>
    </button>
    <button class="tip" data-copy="corgi agent init --config-dir ~/.claude-work">
      <span class="tip-t">Use a second Claude account</span>
      <span class="tip-d">Give a work repo its own account, then set its <b>open in</b> pill to <b>chrome</b>.</span>
      <span class="tip-cmd"><code>corgi agent init --config-dir ~/.claude-work</code><span class="tip-copy">COPY</span></span>
    </button>
    <button class="tip" data-copy="corgi agent install">
      <span class="tip-t">Survive a reboot</span>
      <span class="tip-d">Starts corgi at login, so this page still works when you are away from the desk.</span>
      <span class="tip-cmd"><code>corgi agent install</code><span class="tip-copy">COPY</span></span>
    </button>
    <button class="tip" data-copy="corgi agent hooks enable --all">
      <span class="tip-t">Tell me when a session needs me</span>
      <span class="tip-d">A session waiting on a permission prompt is invisible from here. <b>--all</b> covers every repo in this list. These reach the laptop only — <b>corgi agent notify telegram --token …</b> sends them to this phone too.</span>
      <span class="tip-cmd"><code>corgi agent hooks enable --all</code><span class="tip-copy">COPY</span></span>
    </button>
    <p class="tipnote" id="tipmsg">Tap a row to copy its command.</p>
  </details>

  </div>
  <div data-pane="settings" hidden>
  <details class="settings" id="settings" hidden>
    <summary>Settings</summary>

    <h3>Claude app</h3>
    <p>Add corgi as a custom connector on claude.ai (Settings → Connectors → Add custom) to drive this machine from the Claude app too.</p>
    <pre id="cfg"></pre>
    <button id="copycfg">Copy connector config</button>
    <p id="copymsg" class="msg"></p>

    <h3>This browser</h3>
    <p>The <b>open in</b> pill on each card cycles where session links open: <b>app</b> deep-links into the Claude app, <b>browser</b> keeps them here, <b>chrome</b> forces Chrome — the one to pick for a repo on a different Claude account. Remembered per workspace, here only.</p>
    <p><b>hide</b> tucks a card away when you are showing this screen to someone. Hidden cards collapse into one button; nothing on the machine changes.</p>
    <label class="toggle"><input type="checkbox" id="showbridges"> Show hand-started (bridge) sessions</label>
    <p>A bridge is a session started on the laptop itself. Its claude.ai page shows only what you send from here, so it looks empty at first — the full transcript stays on the laptop.</p>

    <h3>Devices</h3>
    <p>Everything that scanned a pairing QR. Revoking one leaves the others working.</p>
    <div id="devices" class="msg">Loading…</div>

    <h3>If something will not start</h3>
    <p>The same checks as <code>corgi agent doctor</code>, run from here.</p>
    <button id="rundoctor">Run doctor</button>
    <div id="doctor"></div>
    <p>For a push to this phone when a session needs you, set <code>notifyUrl</code> in the agent config on the laptop. A Discord, Slack or Telegram webhook works as well as an ntfy topic — corgi picks the payload from the host, which matters because ntfy's iOS app is paid. Without it, notifications only reach the laptop.</p>
  </details>
  <p class="foot">
    <a id="allsessions" target="_blank" rel="noopener">See all your sessions on claude.ai ↗</a>
  </p>
  </div>
</main>
<div class="toast" id="toast" hidden></div>
<script>
  // Tabs. The pane you left is the pane you come back to, because the phone
  // is usually unlocked to check one thing.
  function showTab(name) {
    for (const b of document.querySelectorAll('#tabs button')) {
      b.setAttribute('aria-selected', String(b.dataset.tab === name));
    }
    for (const p of document.querySelectorAll('[data-pane]')) {
      p.hidden = p.dataset.pane !== name;
    }
    try { localStorage.setItem('corgi.tab', name); } catch {}
  }
  for (const b of document.querySelectorAll('#tabs button')) {
    b.onclick = () => showTab(b.dataset.tab);
  }
  try {
    const saved = localStorage.getItem('corgi.tab');
    if (saved && document.querySelector('[data-pane="' + saved + '"]')) showTab(saved);
  } catch {}

  // A count only earns its place when it is not zero.
  function tabCount(name, n) {
    const el = document.getElementById('n-' + name);
    if (el) el.textContent = n > 0 ? String(n) : '';
  }

  const esc = s => String(s).replace(/[&<>"']/g, c =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  // The session URL is scanned from process output; only ever click through to
  // a real https claude.ai link, never a non-https or off-domain target.
  const safeClaudeUrl = u => {
    try { const p = new URL(u); return p.protocol === 'https:' && (p.hostname === 'claude.ai' || p.hostname.endsWith('.claude.ai')); }
    catch { return false; }
  };
  const token = (() => {
    // corgi agent dashboard puts a token in the fragment, which never
    // reaches the server or its logs. Keep it, then drop it from the address
    // bar so it is not left sitting in history or a shared screenshot.
    try {
      const m = /(?:^|[#&])token=([A-Za-z0-9._-]+)/.exec(location.hash || '');
      if (m) {
        localStorage.setItem('corgi_token', m[1]);
        history.replaceState(null, '', location.pathname + location.search);
        return m[1];
      }
    } catch {}
    try { return localStorage.getItem('corgi_token') || ''; } catch { return ''; }
  })();
  const list = document.getElementById('list');
  const auth = { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json', 'ngrok-skip-browser-warning': '1' };
  // Set in JS (not a static href) so the page source carries no external link;
  // this is a user navigation to the Claude session list, not a loaded asset.
  try { document.getElementById('allsessions').href = 'https://claude.ai/code'; } catch {}

  // One failure, one line, over the thumb — never a red block that pushes the
  // card you were aiming at somewhere else.
  const REFRESH_MS = 15000;
  let toastTimer = 0;
  function toast(text, bad) {
    const el = document.getElementById('toast');
    el.textContent = text;
    el.className = 'toast' + (bad ? ' bad' : '');
    el.hidden = false;
    requestAnimationFrame(() => el.classList.add('on'));
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      el.classList.remove('on');
      setTimeout(() => { el.hidden = true; }, 200);
    }, 3600);
  }

  let lastWorkspaces = [];
  const openMode = id => { try { return localStorage.getItem('corgi_open_' + id) || 'app'; } catch { return 'app'; } };
  const setOpenMode = (id, m) => { try { localStorage.setItem('corgi_open_' + id, m); } catch {} };
  const showBridges = () => { try { return localStorage.getItem('corgi_show_bridges') !== '0'; } catch { return true; } };
  const hidden = () => { try { return new Set(JSON.parse(localStorage.getItem('corgi_hidden') || '[]')); } catch { return new Set(); } };
  const setHidden = s => { try { localStorage.setItem('corgi_hidden', JSON.stringify([...s])); } catch {} };
  const toggleHidden = id => { const h = hidden(); h.has(id) ? h.delete(id) : h.add(id); setHidden(h); render(lastWorkspaces); };
  let revealHidden = false;
  const setShowBridges = on => { try { localStorage.setItem('corgi_show_bridges', on ? '1' : '0'); } catch {} };

  if (!token) {
    // The tabs have nothing to show without a token, and the message used to
    // land in a pane that is hidden unless you happen to be on Stacks — which
    // is how this page came to render as a blank screen.
    document.getElementById('tabs').hidden = true;
    for (const p of document.querySelectorAll('[data-pane]')) {
      p.hidden = true;
    }
    const pair = document.createElement('div');
    pair.className = 'empty';
    pair.innerHTML = '<h2>Pair this browser</h2><p>This page needs a key before it can show ' +
      'anything. On the laptop run <code>corgi agent up</code> and open the <b>pair:</b> link it ' +
      'prints — in this browser for this machine, or by scanning the QR on a phone.</p>' +
      '<p>The key is kept in this browser only, and pairing again replaces it.</p>';
    document.querySelector('main').prepend(pair);
    document.getElementById('refresh').hidden = true;
  } else {
    initSettings();
    loadInfo();
    loadBoard();
    skeleton();
    load();
    initRefresh();
  }

  function skeleton() {
    list.className = '';
    list.innerHTML = '<div class="skel"></div><div class="skel"></div><div class="skel"></div>';
  }

  // A session's name and its state both change while you are looking at them:
  // Claude renames a session as the work takes shape, a permission prompt
  // arrives, a session exits. Refresh on a slow tick — only while the page is
  // on screen, and never while a panel is open under the thumb, since
  // re-rendering would collapse it.
  function initRefresh() {
    const btn = document.getElementById('refresh');
    btn.onclick = () => {
      btn.classList.add('spin');
      Promise.all([load(), loadInfo(), loadBoard()]).finally(() => setTimeout(() => btn.classList.remove('spin'), 400));
    };
    setInterval(autoRefresh, REFRESH_MS);
    addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') autoRefresh();
    });
  }

  function autoRefresh() {
    if (document.visibilityState !== 'visible') return;
    const openPanel = [...document.querySelectorAll('.sessions')].some(el => el.style.display !== 'none');
    if (openPanel || document.querySelector('.startbox.on')) return;
    load();
    loadBoard();
  }

  function initSettings() {
    const s = document.getElementById('settings');
    const connector = JSON.stringify({ mcpServers: { corgi: {
      url: location.origin + '/mcp', headers: { Authorization: 'Bearer ' + token } } } }, null, 2);
    document.getElementById('cfg').textContent = connector;
    s.hidden = false;
    s.open = true;
    document.getElementById('copycfg').onclick = async (e) => {
      const msg = document.getElementById('copymsg');
      try { await navigator.clipboard.writeText(connector); msg.textContent = '✓ Copied'; }
      catch { msg.textContent = 'Long-press the box above to copy.'; }
    };
    const bridges = document.getElementById('showbridges');
    bridges.checked = showBridges();
    bridges.onchange = () => setShowBridges(bridges.checked);
    loadDevices();
    document.getElementById('rundoctor').onclick = runDoctor;
    initTips();
  }

  function initTips() {
    const box = document.getElementById('tips');
    box.hidden = false;
    box.open = true;
    const msg = document.getElementById('tipmsg');
    for (const tip of box.querySelectorAll('.tip')) {
      tip.onclick = async () => {
        const text = tip.dataset.copy;
        try {
          await navigator.clipboard.writeText(text);
          tip.classList.add('copied');
          const label = tip.querySelector('.tip-copy');
          if (label) label.textContent = 'COPIED';
          msg.textContent = 'Copied — paste it in a terminal on that machine.';
          setTimeout(() => {
            tip.classList.remove('copied');
            if (label) label.textContent = 'COPY';
          }, 1400);
        } catch {
          msg.textContent = 'Long-press the command to copy it.';
        }
      };
    }
  }

  async function loadDevices() {
    const box = document.getElementById('devices');
    try {
      const r = await fetch('/launch/devices', { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      const list = j.devices || [];
      if (!list.length) { box.className = 'msg'; box.textContent = 'No devices paired yet.'; return; }
      box.className = ''; box.innerHTML = '';
      for (const d of list) {
        const row = document.createElement('div');
        row.className = 'dev';
        const left = document.createElement('div');
        left.innerHTML = esc(d.name) + (d.current ? ' <span class="sub">· this device</span>' : '') +
          '<div class="sub">paired ' + esc(fmtWhen(d.pairedAt)) + '</div>';
        row.appendChild(left);
        if (!d.current) {
          const b = document.createElement('button');
          b.textContent = 'Revoke';
          b.onclick = () => revokeDevice(d.name, b);
          row.appendChild(b);
        }
        box.appendChild(row);
      }
    } catch (e) { box.className = 'msg err'; box.textContent = '✗ ' + e.message; }
  }

  async function revokeDevice(name, btn) {
    btn.disabled = true; btn.textContent = 'Revoking…';
    try {
      const r = await fetch('/launch/devices?name=' + encodeURIComponent(name), { method: 'DELETE', headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      loadDevices();
    } catch (e) { btn.disabled = false; btn.textContent = '✗ ' + e.message; }
  }

  async function runDoctor() {
    const box = document.getElementById('doctor');
    box.className = 'msg'; box.textContent = 'Checking…';
    try {
      const r = await fetch('/launch/doctor', { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      const checks = (j.checks || []).slice().sort((a, b) => (a.ok === b.ok) ? 0 : (a.ok ? 1 : -1));
      box.className = ''; box.innerHTML = '';
      for (const c of checks) {
        const el = document.createElement('div');
        el.className = 'chk' + (c.ok ? '' : ' bad');
        el.innerHTML = '<span class="mark">' + (c.ok ? '✓' : '✗') + '</span><span><b>' + esc(c.name) + '</b> — ' +
          esc(c.detail || '') + (c.fix ? '<span class="fix">fix: ' + esc(c.fix) + '</span>' : '') + '</span>';
        box.appendChild(el);
      }
    } catch (e) { box.className = 'msg err'; box.textContent = '✗ ' + e.message; }
  }

  async function load() {
    try {
      const r = await fetch('/launch/workspaces', { headers: auth });
      if (r.status === 401) throw new Error('This device is not authorized. Re-pair from corgi agent up.');
      const j = await r.json();
      render(j.workspaces || []);
    } catch (e) {
      list.className = 'msg err'; list.textContent = '✗ ' + e.message;
    }
  }

  // The session board answers the question the phone is usually unlocked
  // for: is anything waiting on me. Sessions needing a person come first
  // and pulse; the rest are one muted line. Nothing tracked, nothing shown.
  const STATUS_WORD = { needs_input: 'needs you', working: 'working', done: 'done', stale: 'idle', gone: 'closed', unknown: '', limited: 'limit' };
  // Each account's 5-hour window: the number the phone is unlocked for when
  // the question is "can I start another task".
  function clock(iso) {
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  }
  function renderAccounts(accounts, box) {
    const known = (accounts || []).filter(a => a.limits && a.limits.fiveHour);
    if (!known.length) return;
    const line = document.createElement('div');
    line.className = 'acct';
    for (const a of known) {
      const pct = a.limits.fiveHour.percent || 0;
      const span = document.createElement('span');
      const name = document.createElement('b'); name.textContent = a.profile || 'default';
      span.appendChild(name);
      const bar = document.createElement('span');
      bar.className = 'bar' + (pct >= 90 ? ' hot' : pct >= 70 ? ' warm' : '');
      const fill = document.createElement('i'); fill.style.width = Math.min(100, pct) + '%';
      bar.appendChild(fill); span.appendChild(bar);
      let text = pct + '%';
      if (a.limits.fiveHour.resetsAt) text += ' · resets ' + clock(a.limits.fiveHour.resetsAt);
      span.appendChild(document.createTextNode(text));
      const f = a.forecast && a.forecast.fiveHour;
      if (f && f.exhaustAt && f.safe === false) {
        const out = document.createElement('span');
        out.className = 'out'; out.textContent = ' · runs out ' + clock(f.exhaustAt);
        span.appendChild(out);
      }
      line.appendChild(span);
    }
    box.appendChild(line);
  }
  async function loadBoard() {
    const box = document.getElementById('board');
    try {
      const r = await fetch('/launch/board', { headers: auth });
      if (!r.ok) { box.hidden = true; return; }
      const j = await r.json();
      renderNewChat(j);
      loadInbox();
      loadKanban();
      const sessions = (j.sessions || []).filter(s => s.status !== 'gone');
      tabCount('sessions', sessions.length);
      if (!sessions.length) { box.hidden = true; return; }
      box.innerHTML = '';
      renderAccounts(j.accounts, box);
      const hot = sessions.filter(s => s.status === 'needs_input');
      const sum = document.createElement('p');
      sum.className = 'sum' + (hot.length ? ' hot' : '');
      const parts = [];
      if (hot.length) parts.push(hot.length + ' waiting on you');
      const working = sessions.filter(s => s.status === 'working').length;
      if (working) parts.push(working + ' working');
      const rest = sessions.length - hot.length - working;
      if (rest) parts.push(rest + ' idle');
      sum.textContent = 'Claude sessions on this machine · ' + parts.join(' · ');
      box.appendChild(sum);
      // All of them, the ones wanting a person first. This was a three-row
      // teaser when the board was squeezed above the workspace list; it has a
      // tab of its own now, and a tab that counts five must show five.
      const rank = { needs_input: 0, working: 1, limited: 2, done: 3, stale: 4, unknown: 5 };
      const shown = sessions.slice().sort((a, b) =>
        (rank[a.status] ?? 9) - (rank[b.status] ?? 9));
      for (const s of shown) {
        const row = document.createElement('div');
        row.className = 'sess';
        const dot = document.createElement('span');
        dot.className = 'sdot ' + (s.status === 'needs_input' ? 'needs' : esc(s.status || 'unknown'));
        const label = document.createElement('span');
        label.className = 'slabel'; label.textContent = s.display || s.label || '?';
        row.appendChild(dot); row.appendChild(label);
        let detail = s.detail || STATUS_WORD[s.status] || '';
        if (s.status === 'limited' && s.limit === 'overload') detail = 'API overloaded';
        if (s.drift && s.drift.length) detail = 'drifting: ' + s.drift[0];
        if (s.resumeAt) detail += (detail ? ' · ' : '') + 'continues ' + clock(s.resumeAt);
        if (s.context && s.context.percent >= 50) detail += (detail ? ' · ' : '') + 'ctx ' + s.context.percent + '%';
        if (detail) {
          const d = document.createElement('span');
          d.className = 'sdetail'; d.textContent = detail;
          row.appendChild(d);
        }
        if (s.profile && s.profile !== 'default') {
          const b = document.createElement('span');
          b.className = 'sbadge'; b.textContent = s.profile;
          row.appendChild(b);
        }
        const sum = sessionSummary(s);
        if (sum) {
          const l = document.createElement('div');
          l.className = 'ssum'; l.textContent = sum;
          row.appendChild(l);
        }
        const act = sessionActions(s);
        if (act) row.appendChild(act);
        box.appendChild(row);
      }
      box.hidden = false;
    } catch { box.hidden = true; }
  }

  // The new-chat card is built once and only its choices refresh, so a
  // prompt being typed survives the board's periodic reload.
  const MODELS = [['', 'default model'], ['opus', 'Opus'], ['sonnet', 'Sonnet'], ['haiku', 'Haiku']];
  function windowLabel(w) {
    const f = (w.folders || [])[0];
    return f ? f.replace(/[\\/]+$/, '').split(/[\\/]/).pop() : (w.app || w.id);
  }
  function fillSelect(sel, options, keep) {
    const current = keep ? sel.value : '';
    sel.innerHTML = '';
    for (const [v, t] of options) {
      const o = document.createElement('option'); o.value = v; o.textContent = t; sel.appendChild(o);
    }
    if (keep && options.some(([v]) => v === current)) sel.value = current;
  }
  function renderNewChat(j) {
    const sec = document.getElementById('newchat');
    const windows = j.windows || [];
    if (!windows.length) { sec.hidden = true; return; }
    let card = sec.querySelector('.newchat');
    if (!card) {
      card = document.createElement('div'); card.className = 'newchat';
      const ta = document.createElement('textarea');
      ta.rows = 2; ta.placeholder = 'New chat on the laptop: what should Claude do?'; ta.className = 'nprompt';
      const row = document.createElement('div'); row.className = 'nrow';
      const win = document.createElement('select'); win.className = 'nwin';
      const ws = document.createElement('select'); ws.className = 'nws';
      const model = document.createElement('select'); model.className = 'nmodel';
      const prof = document.createElement('select'); prof.className = 'nprof';
      fillSelect(model, MODELS, false);
      try { const m = localStorage.getItem('corgi.newchat.model'); if (m !== null) model.value = m; } catch {}
      model.onchange = () => { try { localStorage.setItem('corgi.newchat.model', model.value); } catch {} };
      const go = document.createElement('button'); go.type = 'button'; go.className = 'ok'; go.textContent = 'Start chat';
      go.onclick = async () => {
        const prompt = ta.value.trim();
        go.disabled = true;
        try {
          const r = await fetch('/launch/new', { method: 'POST', headers: auth,
            body: JSON.stringify({ window: win.value, prompt, model: model.value, profile: prof.value, workspace: ws.value }) });
          const jj = await r.json().catch(() => ({}));
          if (!r.ok) { toast(jj.error || 'could not open a chat', true); return; }
          toast('opening a chat in ' + (win.selectedOptions[0] || {}).textContent + (prompt ? ' with your prompt' : ''));
          ta.value = '';
          setTimeout(loadBoard, 4000);
        } catch { toast('no connection', true); }
        finally { go.disabled = false; }
      };
      row.appendChild(win); row.appendChild(ws); row.appendChild(model); row.appendChild(prof); row.appendChild(go);
      card.appendChild(ta); card.appendChild(row);
      sec.appendChild(card);
    }
    const front = j.frontWindow || j.lastFocusWindow || '';
    const win = card.querySelector('.nwin');
    const had = win.options.length > 0;
    fillSelect(win, windows.map(w => [w.id, windowLabel(w)]), had);
    if (!had && front && windows.some(w => w.id === front)) win.value = front;
    const profiles = (j.accounts || []).map(a => a.profile).filter(p => p && p !== 'default');
    const prof = card.querySelector('.nprof');
    fillSelect(prof, [['', 'default account'], ...profiles.map(p => [p, p])], true);
    prof.hidden = !profiles.length;
    // Which checkout it opens in. A phone has no folder of its own, so
    // without this the chat lands wherever that editor window happened to
    // be — the wrong repo, under the wrong account.
    const ws = card.querySelector('.nws');
    let ids = lastWorkspaces.map(w => w.id).filter(Boolean);
    if (!ids.length) {
      // The board arrives before the workspace list on a cold load; the
      // sessions name their workspaces, which is enough to choose one.
      ids = [...new Set((j.sessions || []).map(s => s.workspace || s.display).filter(Boolean))];
    }
    fillSelect(ws, [['', 'window\u2019s folder'], ...ids.map(i => [i, i])], true);
    if (!ws.value) {
      try { const saved = localStorage.getItem('corgi.newchat.workspace'); if (saved && ids.includes(saved)) ws.value = saved; } catch {}
    }
    ws.onchange = () => { try { localStorage.setItem('corgi.newchat.workspace', ws.value); } catch {} };
    ws.hidden = !ids.length;
    sec.hidden = false;
  }

  function sessionSummary(s) {
    const bits = [];
    if (s.branch) bits.push(s.branch);
    if (s.status === 'working' && s.turnStartedAt) {
      const m = Math.floor((Date.now() - Date.parse(s.turnStartedAt)) / 60000);
      if (m >= 1) bits.push('turn ' + m + 'm');
    }
    if (s.summary) bits.push(s.summary);
    return bits.join(' \u00b7 ');
  }

  function sessionActions(s) {
    const box = document.createElement('div');
    box.className = 'sact';
    const name = s.display || s.label || 'the session';
    const button = (text, cls, fn) => {
      const b = document.createElement('button');
      b.type = 'button'; b.className = cls; b.textContent = text; b.onclick = fn;
      box.appendChild(b);
    };
    if (s.status === 'needs_input' && s.pending) {
      button('Allow', 'ok', () => boardAction('answer', { session: s.id, answer: 'allow' }));
      button('Always', '', () => boardAction('answer', { session: s.id, answer: 'always' }));
      button('Deny', 'bad', () => boardAction('answer', { session: s.id, answer: 'deny' }));
    } else if (s.status !== 'gone') {
      button('Send\u2026', '', () => {
        const text = window.prompt('Type into ' + name);
        if (text && text.trim()) boardAction('send', { session: s.id, text: text.trim() });
      });
    }
    // A drifting session gets the one way out the phone can offer: a clean
    // restart from a handoff, same account, same worktree.
    if (s.drift && s.drift.length && s.status !== 'gone') {
      button('Fresh', 'bad', async (e) => {
        if (!confirm('Restart ' + name + ' clean from a handoff?\n' + s.drift.join('\n'))) return;
        const b = e.currentTarget; b.disabled = true;
        try {
          const r = await fetch('/launch/fresh', { method: 'POST', headers: auth, body: JSON.stringify({ session: s.id }) });
          const j = await r.json().catch(() => ({}));
          if (!r.ok) { toast(j.error || 'could not restart it', true); b.disabled = false; return; }
          toast(j.done || 'restarting'); setTimeout(loadBoard, 4000);
        } catch { toast('no connection', true); b.disabled = false; }
      });
    }
    if (s.pr) {
      const a = document.createElement('a');
      a.href = s.pr; a.target = '_blank'; a.rel = 'noopener'; a.textContent = 'PR';
      box.appendChild(a);
    }
    // The row had no way to open the session it names, which is the first
    // thing anyone taps it for.
    if (s.url && safeClaudeUrl(s.url)) {
      box.appendChild(sessionOpener(s.label || s.display, s.url, 'Open \u2197', ''));
    } else if (s.id && (s.status === 'done' || s.status === 'stale')) {
      // No web link yet: corgi can ask the session for one. Only while it is
      // idle — typing into a session mid-turn lands in its own work.
      button('Link', '', async (e) => {
        const b = e.currentTarget;
        b.disabled = true; b.textContent = 'Linking\u2026';
        try {
          const r = await fetch('/launch/send', { method: 'POST', headers: auth,
            body: JSON.stringify({ session: s.id, text: '/remote-control' }) });
          const j = await r.json().catch(() => ({}));
          if (!r.ok) { toast(j.error || 'could not reach that session', true); b.disabled = false; b.textContent = 'Link'; return; }
          toast('asked ' + name + ' for a web link — it appears in a moment');
          setTimeout(loadBoard, 6000);
        } catch { toast('no connection', true); b.disabled = false; b.textContent = 'Link'; }
      });
    }
    return box.childElementCount ? box : null;
  }

  const EVENT_KIND = { 'issue.new': 'new issue', 'issue.comment': 'comment', 'pr.comment': 'PR comment', 'pr.review': 'PR review' };

  // One change to one ticket. Every call here is something someone tapped.
  async function ticket(ev, body, quiet) {
    try {
      const r = await fetch('/launch/ticket', { method: 'POST', headers: auth,
        body: JSON.stringify(Object.assign({ key: ev.key }, body)) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { if (!quiet) toast(j.error || 'that did not go through', true); return false; }
      // A batch says one thing at the end rather than a toast per row.
      if (!quiet) { toast(j.done || 'done'); setTimeout(() => { loadInbox(); loadKanban(); }, 600); }
      return true;
    } catch { if (!quiet) toast('no connection', true); return false; }
  }

  // The columns as a sheet: a phone has no room for twelve buttons in a row,
  // and the list is the workspace's real board, not a guess.
  function moveSheet(ev, columns, onPick) {
    const scrim = document.createElement('div');
    scrim.className = 'scrim';
    const sheet = document.createElement('div');
    sheet.className = 'sheet';
    const close = () => scrim.remove();
    scrim.onclick = e => { if (e.target === scrim) close(); };
    const grab = document.createElement('div'); grab.className = 'grab';
    const h = document.createElement('h3');
    h.textContent = 'Move ' + (ev.ref || ev.key) + ' to';
    sheet.append(grab, h);
    for (const name of columns) {
      const b = document.createElement('button');
      const here = ev.state && name.toLowerCase() === String(ev.state).toLowerCase();
      b.className = 'opt' + (here ? ' here' : '');
      b.textContent = here ? name + ' · it is here' : name;
      b.disabled = here;
      b.onclick = async () => {
        b.disabled = true;
        if (onPick) { close(); await onPick(name); return; }
        if (await ticket(ev, { do: 'move', status: name })) close(); else b.disabled = false;
      };
      sheet.appendChild(b);
    }
    const mine = document.createElement('button');
    mine.className = 'opt';
    mine.textContent = 'Assign to me';
    mine.hidden = !!onPick; // assigning a batch is a different ask
    mine.onclick = async () => {
      mine.disabled = true;
      if (await ticket(ev, { do: 'assign' })) close(); else mine.disabled = false;
    };
    sheet.appendChild(mine);
    scrim.appendChild(sheet);
    document.body.appendChild(scrim);
  }

  // What a run said, on the phone: the tail of its log, the handoff it left,
  // what it opened and what it cost. The one screen for "what happened".
  function humanTokens(n) {
    n = Number(n) || 0;
    if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n >= 1e3) return Math.round(n / 1e3) + 'k';
    return String(n);
  }

  async function runSheet(fx) {
    const scrim = document.createElement('div');
    scrim.className = 'scrim';
    const sheet = document.createElement('div');
    sheet.className = 'sheet';
    const close = () => scrim.remove();
    scrim.onclick = e => { if (e.target === scrim) close(); };
    const grab = document.createElement('div'); grab.className = 'grab';
    const h = document.createElement('h3');
    h.textContent = fx.ref + (fx.running ? ' · running' : '');
    const pre = document.createElement('pre');
    pre.className = 'runlog';
    pre.textContent = 'loading…';
    sheet.append(grab, h, pre);
    scrim.appendChild(sheet);
    document.body.appendChild(scrim);
    try {
      const r = await fetch('/launch/run?key=' + encodeURIComponent(fx.key) + '&tail=200', { headers: auth });
      const j = await r.json();
      if (!r.ok) { pre.textContent = j.error || 'no log'; return; }
      const meta = document.createElement('p');
      meta.className = 'etitle';
      const bits = [];
      if (j.outcome) bits.push(j.outcome);
      if (j.tokens) bits.push(humanTokens(j.tokens) + ' tok' + (j.costUSD ? ' · $' + j.costUSD.toFixed(2) : ''));
      if (j.spentPercent) bits.push(j.spentPercent + '% of the 5h window');
      if (j.handover) bits.push(j.handover);
      meta.textContent = bits.join(' · ');
      sheet.insertBefore(meta, pre);
      pre.textContent = j.log || '(the log is empty)';
      pre.scrollTop = pre.scrollHeight;
    } catch { pre.textContent = 'no connection'; }
  }

  // The kanban: one card per ticket, column worked out by corgi. A card is
  // moved on the tracker, worked on, or unblocked — the column follows.
  async function loadKanban() {
    const box = document.getElementById('kanban');
    let cards = [], columns = [], boards = {};
    try {
      const r = await fetch('/launch/kanban', { headers: auth });
      if (!r.ok) { box.hidden = true; return; }
      const j = await r.json();
      cards = j.cards || []; columns = j.columns || []; boards = j.boards || {};
    } catch { box.hidden = true; tabCount('kanban', 0); return; }
    const live = cards.filter(c => c.column !== 'Done').length;
    tabCount('kanban', live);
    if (!cards.length) { box.hidden = true; return; }
    box.hidden = false;
    box.innerHTML = '';
    const wrap = document.createElement('div');
    wrap.className = 'kb';
    for (const col of columns) {
      const rows = cards.filter(c => c.column === col);
      const kc = document.createElement('div');
      kc.className = 'kcol';
      const h = document.createElement('h3');
      h.textContent = col;
      const n = document.createElement('span'); n.textContent = String(rows.length);
      h.appendChild(n);
      kc.appendChild(h);
      if (!rows.length) {
        const e = document.createElement('div'); e.className = 'empty'; e.textContent = '—';
        kc.appendChild(e);
      }
      for (const c of rows) {
        const card = document.createElement('div');
        card.className = 'kcard' + (col === 'Blocked' ? ' blocked' : col === 'Running' ? ' running' : '');
        const head = document.createElement('div');
        const ref = document.createElement('span'); ref.className = 'kref'; ref.textContent = c.ref;
        head.appendChild(ref);
        if (c.workspace) { const w = document.createElement('span'); w.className = 'kws'; w.textContent = c.workspace; head.appendChild(w); }
        card.appendChild(head);
        if (c.title) { const t = document.createElement('p'); t.className = 'ktitle'; t.textContent = c.title; card.appendChild(t); }
        const why = document.createElement('p'); why.className = 'kwhy'; why.textContent = c.why || ''; card.appendChild(why);
        if (c.session) { const m = document.createElement('p'); m.className = 'kmeta'; m.textContent = 'session ' + c.session.label + ' · ' + (STATUS_WORD[c.session.status] || c.session.status); card.appendChild(m); }
        if (c.branch) { const m = document.createElement('p'); m.className = 'kmeta'; m.textContent = c.branch; card.appendChild(m); }
        if (c.handoff && c.handoff.next) { const m = document.createElement('p'); m.className = 'kmeta'; m.textContent = 'next: ' + c.handoff.next; card.appendChild(m); }
        if (c.cost) {
          const bits = [humanTokens(c.cost.tokens) + ' tok'];
          if (c.cost.usd) bits.push('$' + c.cost.usd.toFixed(2));
          if (c.cost.runs) bits.push(c.cost.runs + (c.cost.runs === 1 ? ' run' : ' runs'));
          if (c.cost.sessions) bits.push(c.cost.sessions + (c.cost.sessions === 1 ? ' session' : ' sessions'));
          const m = document.createElement('p'); m.className = 'kmeta'; m.textContent = bits.join(' · '); card.appendChild(m);
        }
        const row = document.createElement('div'); row.className = 'erow';
        const ev = { key: c.key, ref: c.ref, workspace: c.workspace, kind: c.kind, url: c.url };
        if (c.url && /^https:\/\//.test(c.url)) {
          const a = document.createElement('a'); a.className = 'eopen'; a.href = c.url; a.target = '_blank'; a.rel = 'noopener noreferrer'; a.textContent = 'Open'; row.appendChild(a);
        }
        if (c.fix && (c.fix.prs || []).length) {
          const a = document.createElement('a'); a.className = 'eopen'; a.href = c.fix.prs[0]; a.target = '_blank'; a.rel = 'noopener noreferrer'; a.textContent = 'Open PR'; row.appendChild(a);
        }
        if (col === 'Blocked' && c.key) {
          const ub = document.createElement('button'); ub.className = 'primary'; ub.textContent = 'Unblock';
          ub.onclick = () => { ub.disabled = true; ticket(ev, { do: 'unblock' }).finally(() => { ub.disabled = false; loadKanban(); }); };
          row.appendChild(ub);
        } else if ((col === 'Inbox' || col === 'Ready') && c.key && c.kind && c.kind.startsWith('issue.')) {
          const go = document.createElement('button'); go.className = 'primary'; go.textContent = 'Work on it';
          go.onclick = () => { go.disabled = true; workOn(ev).finally(() => { go.disabled = false; loadKanban(); }); };
          row.appendChild(go);
        }
        const board = boards[c.workspace] || {};
        if ((board.columns || []).length && c.key) {
          const mv = document.createElement('button'); mv.textContent = 'Move…';
          mv.onclick = () => moveSheet(ev, board.columns);
          row.appendChild(mv);
        }
        if (row.children.length) card.appendChild(row);
        kc.appendChild(card);
      }
      wrap.appendChild(kc);
    }
    box.appendChild(wrap);
  }

  async function loadInbox() {
    const box = document.getElementById('inbox');
    let events = [], fixes = [], boards = {};
    try {
      const r = await fetch('/launch/events', { headers: auth });
      if (!r.ok) { box.hidden = true; return; }
      const j = await r.json();
      events = j.events || [];
      fixes = j.fixes || [];
      boards = j.boards || {};
    } catch { box.hidden = true; tabCount('inbox', 0); return; }
    tabCount('inbox', events.length);
    if (!events.length && !fixes.length) { box.hidden = true; return; }

    box.innerHTML = '';
    // One line before the list: what is waiting, and what corgi did about it.
    // The phone is unlocked to read exactly this.
    const tally = document.createElement('p');
    tally.className = 'sum hot';
    const running = fixes.filter((f) => f.running).length;
    const toReview = fixes.filter((f) => !f.running && (f.prs || []).length).length;
    const bits = [];
    if (events.length) bits.push(events.length + (events.length === 1 ? ' thing waiting on you' : ' things waiting on you'));
    if (toReview) bits.push(toReview + (toReview === 1 ? ' pull request corgi opened' : ' pull requests corgi opened'));
    if (running) bits.push(running + ' still running');
    if (bits.length) {
      tally.textContent = bits.join(' \u00b7 ');
      box.appendChild(tally);
    }
    const sum = document.createElement('p');
    sum.className = 'sum';
    sum.textContent = 'From the tracker and your pull requests';
    box.appendChild(sum);

    // One heading per workspace: a batch is shipped against one checkout.
    const groups = new Map();
    for (const ev of events.slice(0, 12)) {
      const key = ev.workspace || '';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(ev);
    }

    for (const [workspace, rows] of groups) {
      const picked = new Set();
      // The group is one workspace, so its columns are the batch's columns.
      const board = boards[workspace] || {};
      const head = document.createElement('div');
      head.className = 'evgroup';
      const name = document.createElement('span');
      name.className = 'gname';
      name.textContent = workspace || 'elsewhere';
      // Selecting rows was only ever wired to one verb. Clearing a morning's
      // worth of noise one Ignore at a time is the thing people actually do
      // most, and it was the one thing the checkboxes could not do.
      const bar = document.createElement('div');
      bar.className = 'batch';
      bar.hidden = true;
      const count = document.createElement('span');
      count.className = 'bcount';
      const batch = document.createElement('button');
      batch.className = 'primary';
      const moveMany = document.createElement('button');
      moveMany.textContent = 'Move\u2026';
      const ignoreMany = document.createElement('button');
      ignoreMany.textContent = 'Ignore';
      bar.append(count, batch, moveMany, ignoreMany);
      head.append(name);
      box.appendChild(head);
      box.appendChild(bar);

      // Only new issues can be handed to one session together — a review
      // comment is about its own thread — but anything can be moved or
      // dismissed in a batch.
      const workable = () => [...picked].filter((k) => {
        const ev = rows.find((r) => r.key === k);
        return ev && ev.actionable && ev.kind === 'issue.new';
      });
      const refresh = () => {
        bar.hidden = picked.size === 0;
        count.textContent = picked.size + ' selected';
        const canShip = workable().length;
        batch.hidden = canShip < 2;
        batch.textContent = 'Ship ' + canShip + ' together';
        moveMany.hidden = !(board.columns || []).length;
      };
      batch.onclick = () => {
        batch.disabled = true;
        const keys = workable();
        workOn({ keys, ref: keys.length + ' issues' }).finally(() => { batch.disabled = false; });
      };
      ignoreMany.onclick = async () => {
        const keys = [...picked];
        if (!confirm('Ignore ' + keys.length + (keys.length === 1 ? ' row?' : ' rows?') +
          '\nThey leave the inbox and the unattended mode skips them. Nothing is written to the tracker.')) return;
        ignoreMany.disabled = true;
        for (const key of keys) {
          await ticket({ key }, { do: 'ignore' }, true);
        }
        picked.clear();
        toast(keys.length + (keys.length === 1 ? ' row ignored' : ' rows ignored'));
        ignoreMany.disabled = false;
        loadInbox();
      };
      moveMany.onclick = () => {
        const keys = [...picked];
        moveSheet({ ref: keys.length + (keys.length === 1 ? ' ticket' : ' tickets'), keys },
          board.columns, async (status) => {
            let done = 0;
            for (const key of keys) {
              if (await ticket({ key }, { do: 'move', status }, true)) done++;
            }
            picked.clear();
            toast(done + ' of ' + keys.length + ' moved to ' + status);
            loadInbox();
          });
      };

      for (const ev of rows) {
        const card = document.createElement('div');
        card.className = 'ev';

        const head2 = document.createElement('div');
        head2.style.display = 'flex';
        head2.style.alignItems = 'center';
        head2.style.gap = '.4rem';
        // Anything can be selected: a batch can be moved or dismissed even
        // when it cannot be handed to one session.
        {
          const tick = document.createElement('input');
          tick.type = 'checkbox';
          tick.onchange = () => { tick.checked ? picked.add(ev.key) : picked.delete(ev.key); refresh(); };
          head2.appendChild(tick);
        }
        const ref = document.createElement('span');
        ref.className = 'eref';
        ref.textContent = ev.ref || ev.key;
        const kind = document.createElement('span');
        kind.className = 'ekind';
        kind.textContent = EVENT_KIND[ev.kind] || ev.kind;
        head2.append(ref, kind);
        // Which column it sits in. Without it a move you just made looks
        // like it did nothing.
        if (ev.state) {
          const st = document.createElement('span');
          st.className = 'estate';
          st.textContent = ev.state;
          head2.appendChild(st);
        }
        card.appendChild(head2);

        if (ev.title) {
          const t = document.createElement('p');
          t.className = 'etitle';
          t.textContent = ev.title;
          card.appendChild(t);
        }
        // Blocked is a column of its own: the reason, and the one button
        // that puts the ticket back in front of the unattended mode.
        if (ev.blocked) {
          const b = document.createElement('p');
          b.className = 'eblocked';
          b.textContent = 'Blocked' + (ev.blockedBy === 'breaker' ? ' after repeated failures' : ev.blockedBy === 'run' ? ' by the run' : '') + ': ' + ev.blocked;
          card.appendChild(b);
        }

        const row = document.createElement('div');
        row.className = 'erow';
        if (ev.blocked) {
          const ub = document.createElement('button');
          ub.className = 'primary';
          ub.textContent = 'Unblock';
          ub.onclick = () => {
            ub.disabled = true;
            ticket(ev, { do: 'unblock' }).finally(() => { ub.disabled = false; });
          };
          row.appendChild(ub);
        }
        if (ev.url && /^https:\/\//.test(ev.url)) {
          const a = document.createElement('a');
          a.className = 'eopen';
          a.href = ev.url;
          a.target = '_blank';
          a.rel = 'noopener noreferrer';
          a.textContent = ev.kind && ev.kind.startsWith('pr.') ? 'Open PR' : 'Open issue';
          row.appendChild(a);
        }
        if (ev.actionable && !ev.blocked) {
          const go = document.createElement('button');
          go.className = 'primary';
          go.textContent = 'Work on it';
          go.onclick = () => {
            go.disabled = true;
            workOn(ev).finally(() => { go.disabled = false; });
          };
          row.appendChild(go);
        }
        if ((board.columns || []).length) {
          const mv = document.createElement('button');
          mv.textContent = 'Move…';
          mv.onclick = () => moveSheet(ev, board.columns);
          row.appendChild(mv);
        }
        const ig = document.createElement('button');
        ig.textContent = 'Ignore';
        ig.onclick = () => {
          ig.disabled = true;
          ticket(ev, { do: 'ignore' }).finally(() => { ig.disabled = false; });
        };
        row.appendChild(ig);
        if (row.children.length) card.appendChild(row);
        box.appendChild(card);
      }
    }

    // What the unattended mode did while nobody watched, and what it opened.
    // Grouped by day, because "corgi opened this while you slept" and "corgi
    // opened this last week" are not the same news.
    const dayOf = (iso) => {
      const t = Date.parse(iso || '');
      if (Number.isNaN(t)) return 'earlier';
      const d = new Date(t), now = new Date();
      const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
      if (t >= midnight) return 'today';
      if (t >= midnight - 86400000) return 'yesterday';
      return 'earlier';
    };
    for (const bucket of ['today', 'yesterday', 'earlier']) {
      const group = fixes.filter((f) => dayOf(f.startedAt) === bucket);
      if (!group.length) continue;
      renderFixGroup(bucket, group);
    }
    function renderFixGroup(bucket, fixes) {
      const head = document.createElement('div');
      head.className = 'evgroup';
      const name = document.createElement('span');
      name.className = 'gname';
      const opened = fixes.filter((f) => (f.prs || []).length).length;
      name.textContent = bucket === 'today' ? 'corgi did today' :
        bucket === 'yesterday' ? 'corgi did yesterday' : 'corgi did earlier';
      head.appendChild(name);
      if (opened) {
        const n = document.createElement('span');
        n.className = 'gcount';
        n.textContent = opened + (opened === 1 ? ' waiting on your review' : ' waiting on your review');
        head.appendChild(n);
      }
      box.appendChild(head);
      for (const fx of fixes) {
        const card = document.createElement('div');
        card.className = 'ev';
        const ref = document.createElement('div');
        ref.className = 'eref';
        ref.textContent = fx.ref;
        card.appendChild(ref);
        const t = document.createElement('p');
        t.className = 'etitle';
        t.textContent = fx.running ? 'running…' : (fx.error ? 'failed: ' + fx.error : (fx.prs || []).length ? '' : 'done, nothing opened');
        if (t.textContent) card.appendChild(t);
        const row = document.createElement('div');
        row.className = 'erow';
        const log = document.createElement('button');
        log.textContent = 'Log';
        log.onclick = () => runSheet(fx);
        row.appendChild(log);
        for (const pr of fx.prs || []) {
          if (!/^https:\/\//.test(pr)) continue;
          const a = document.createElement('a');
          a.className = 'eopen';
          a.href = pr; a.target = '_blank'; a.rel = 'noopener noreferrer';
          a.textContent = pr.includes('/pull/') ? 'Open PR' : 'Open MR';
          row.appendChild(a);
          // Finishing corgi's own work without leaving the row. One tap,
          // and only on what corgi opened.
          const merge = document.createElement('button');
          merge.className = 'primary';
          merge.textContent = 'Merge';
          merge.onclick = () => {
            if (!confirm('Merge ' + fx.ref + '?\n' + pr)) return;
            merge.disabled = true;
            ticket({ key: fx.key, ref: fx.ref }, { do: 'merge' }).finally(() => { merge.disabled = false; });
          };
          row.appendChild(merge);
          const shut = document.createElement('button');
          shut.textContent = 'Close';
          shut.onclick = () => {
            if (!confirm('Close ' + fx.ref + ' without merging?\n' + pr)) return;
            shut.disabled = true;
            ticket({ key: fx.key, ref: fx.ref }, { do: 'close' }).finally(() => { shut.disabled = false; });
          };
          row.appendChild(shut);
          break;
        }
        if (row.children.length) card.appendChild(row);
        box.appendChild(card);
      }
    }
    box.hidden = false;
  }

  async function workOn(ev) {
    // Reuse whatever the new-chat box is set to, so a window, model and
    // account picked once apply here too.
    const body = ev.keys ? { keys: ev.keys } : { key: ev.key };
    const pick = (cls) => {
      const el = document.querySelector('#newchat .' + cls);
      return el && el.value ? el.value : '';
    };
    const win = pick('nwin'), model = pick('nmodel'), profile = pick('nprof');
    if (win) body.window = win;
    if (model) body.model = model;
    if (profile) body.profile = profile;
    try {
      const r = await fetch('/launch/work-on', { method: 'POST', headers: auth, body: JSON.stringify(body) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { toast(j.error || 'could not start it', true); return; }
      toast('working on ' + (ev.ref || 'it'));
      setTimeout(loadBoard, 1500);
    } catch { toast('no connection', true); }
  }

  async function boardAction(kind, body) {
    try {
      const r = await fetch('/launch/' + kind, { method: 'POST', headers: auth, body: JSON.stringify(body) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { toast(j.error || 'could not reach the daemon', true); return; }
      toast(kind === 'answer' ? 'answered ' + body.answer : 'sent');
      setTimeout(loadBoard, 1500);
    } catch { toast('no connection', true); }
  }

  async function loadInfo() {
    try {
      const r = await fetch('/launch/info', { headers: auth });
      const j = await r.json();
      if (!r.ok) return;
      const bits = [];
      if (j.host) bits.push(j.host);
      if (j.version) bits.push('corgi ' + j.version);
      bits.push(j.daemon ? 'daemon up' : 'daemon down');
      if (j.latest) bits.push('v' + j.latest + ' available — corgi upd');
      const el = document.getElementById('host');
      el.textContent = '';
      bits.forEach((bit, i) => {
        if (i) el.appendChild(document.createTextNode(' \u00b7 '));
        if (bit.indexOf('daemon ') === 0) {
          const tap = document.createElement('span');
          tap.className = 'what'; tap.textContent = bit;
          tap.onclick = () => toggleDaemonNote(j.daemon);
          el.appendChild(tap);
          return;
        }
        el.appendChild(document.createTextNode(bit));
      });
      if (!j.daemon) el.style.color = 'var(--red)';
    } catch {}
  }

  function toggleDaemonNote(up) {
    const note = document.getElementById('hostnote');
    if (!note.hidden) { note.hidden = true; return; }
    note.textContent = up
      ? 'The corgi daemon is the process on that machine that starts your sessions and keeps them running — after a crash, after a reboot, and while the laptop would otherwise sleep. This page talks to it.'
      : 'The corgi daemon is not running on that machine, so nothing here can start a session. On the laptop run: corgi agent up';
    note.hidden = false;
  }

  function render(workspaces) {
    lastWorkspaces = workspaces;
    tabCount('stacks', workspaces.length);
    if (!workspaces.length) {
      list.className = 'empty';
      list.innerHTML = '<h2>No repos yet</h2><p>On the laptop run <code>corgi agent scan ~/dev</code> ' +
        'to register the stacks it finds, then tap refresh.</p>';
      return;
    }
    list.className = '';
    list.innerHTML = '';
    const intro = introLine(workspaces);
    if (intro) list.appendChild(intro);
    const hide = hidden();
    const shownWs = revealHidden ? workspaces : workspaces.filter(w => !hide.has(w.id));
    const hiddenCount = workspaces.length - shownWs.length;
    for (const ws of shownWs) {
      const row = document.createElement('div');
      row.className = 'ws';
      const head = document.createElement('div');
      head.className = 'head';
      const main = document.createElement('div');
      main.className = 'main';
      main.innerHTML = '<div class="name"><span class="dot ' + esc(dotState(ws)) + '"></span>' + esc(ws.id) + '</div>';
      const path = document.createElement('div');
      path.className = 'path'; path.title = ws.path;
      const short = () => shortPath(ws.path, !!ws.branch) + branchSuffix(ws);
      path.textContent = short();
      path.onclick = () => {
        const full = path.classList.toggle('full');
        path.textContent = full ? ws.path + branchSuffix(ws) : short();
      };
      main.appendChild(path);
      const meta = metaLine(ws);
      if (meta) main.appendChild(meta);
      const usage = usageLine(ws);
      if (usage) main.appendChild(usage);
      head.appendChild(main);
      if (ws.sessionUrl && safeClaudeUrl(ws.sessionUrl) && ws.state !== 'ready') {
        head.appendChild(openControl(ws));
      } else {
        const b = document.createElement('button');
        // Follow the state word the daemon computed. Deriving the label from
        // the running flag instead left a workspace with live local sessions
        // but no link to open stuck on a disabled "Starting…" for good: it is
        // running, it is not ready, and nothing was ever pending.
        const pending = ws.state === 'starting';
        b.textContent = pending ? 'Starting…' : (ws.note && !ws.running ? 'Retry' : 'Start');
        b.disabled = pending;
        b.onclick = () => startSession(ws.id, b);
        head.appendChild(b);
      }
      row.appendChild(head);
      if (!ws.running && ws.note) {
        const note = document.createElement('div');
        note.className = 'wnote'; note.textContent = ws.note;
        row.appendChild(note);
      }
      const actions = document.createElement('div');
      actions.className = 'actions';
      const startBox = startOptions(ws);
      const sessionsBox = document.createElement('div');
      sessionsBox.className = 'sessions'; sessionsBox.style.display = 'none';
      const sbtn = document.createElement('button');
      sbtn.className = 'chip'; sbtn.textContent = sessionsChipLabel(ws);
      sbtn.onclick = () => toggleSessions(ws, sbtn, sessionsBox);
      actions.appendChild(sbtn);
      actions.appendChild(modeSwitch(ws.id));
      const top = topSessionRow(ws);
      if (top) row.appendChild(top);
      if (!ws.running && startBox) {
        const opts = document.createElement('button');
        opts.className = 'chip'; opts.textContent = 'options ⌄';
        opts.onclick = () => { startBox.classList.toggle('on'); };
        actions.appendChild(opts);
      }
      if (ws.running) actions.appendChild(stopControl(ws.id));
      const hideBtn = document.createElement('button');
      hideBtn.className = 'chip';
      hideBtn.title = 'Hide this workspace on this browser';
      hideBtn.textContent = hide.has(ws.id) ? 'unhide' : 'hide';
      hideBtn.onclick = () => toggleHidden(ws.id);
      actions.appendChild(hideBtn);
      if (startBox) actions.appendChild(startBox);
      row.appendChild(actions);
      row.appendChild(sessionsBox);
      list.appendChild(row);
    }
    if (hiddenCount || revealHidden) {
      const b = document.createElement('button');
      b.className = 'chip revealer';
      b.textContent = revealHidden ? 'hide again' : hiddenCount + ' hidden — show';
      b.onclick = () => { revealHidden = !revealHidden; render(lastWorkspaces); };
      list.appendChild(b);
    }
  }

  function introLine(workspaces) {
    if (workspaces.some(w => (w.live || 0) > 0)) return null;
    const el = document.createElement('div');
    el.className = 'intro';
    el.textContent = 'Tap a repo to start a Claude Code session on that machine. ' +
      'It runs there with your files and databases; you drive it from here.';
    return el;
  }

  function sessionsChipLabel(ws) {
    const more = (ws.live || 0) - (ws.topSession ? 1 : 0);
    return more > 0 ? 'sessions +' + more + ' \u2304' : 'sessions \u2304';
  }

  // Inside one repo's card the leading workspace name is noise — the card
  // already says which repo this is, so the branch and the time get the width.
  function shortSessionName(name, id) {
    const full = String(name || '');
    const prefix = id + ' \u00b7 ';
    return full.indexOf(prefix) === 0 ? full.slice(prefix.length) : full;
  }

  // Claude Code owns a session's name after it starts — /rename, a hook, or its
  // own naming all rewrite the record corgi reads — so this row shows what the
  // session is called right now, and says when that last changed.
  function nameNote(top) {
    if (!top.nameSource || top.nameSource === 'user') return '';
    if (!top.nameSince || !top.startedAt || top.nameSince - top.startedAt < 5000) return '';
    return 'renamed ' + fmtWhen(top.nameSince);
  }

  function topSessionRow(ws) {
    const top = ws.topSession;
    if (!top) return null;
    const when = [top.where, top.startedAt ? fmtWhen(top.startedAt) : '']
      .filter(Boolean).join(' \u00b7 ');

    if (!top.url || !safeClaudeUrl(top.url)) {
      const el = document.createElement('div');
      el.className = 'top';
      el.innerHTML = '<span><i class="sdot"></i><span class="tname">' +
        esc(shortSessionName(top.name, ws.id)) + '</span></span>' +
        '<span class="when">' + esc(when) + ' \u00b7 local only</span>';
      return el;
    }
    const el = document.createElement('a');
    el.className = 'top';
    el.href = top.url; el.target = '_blank'; el.rel = 'noopener noreferrer';
    const mode = openMode(ws.id);
    if (mode === 'browser') el.onclick = (e) => { e.preventDefault(); window.open(top.url, '_blank', 'noopener'); };
    if (mode === 'chrome') el.onclick = (e) => { e.preventDefault(); location.href = chromeUrl(top.url); };
    el.innerHTML = '<span><i class="sdot"></i><span class="tname">' +
      esc(shortSessionName(top.name, ws.id)) + '</span></span>' +
      '<span class="when">' + esc(when) + ' \u00b7 open \u2197</span>';
    return el;
  }

  // The state word comes from the daemon (/launch/workspaces), so the phone
  // and anything else reading it say the same thing about the same workspace.
  function dotState(ws) {
    const state = ws.state || (ws.running ? 'starting' : 'stopped');
    return state === 'live' || state === 'ready' || state === 'starting' || state === 'attention' || state === 'blocked'
      ? state : '';
  }

  function metaLine(ws) {
    const bits = [];
    if (ws.live > 0) bits.push('<span class="live">' + ws.live + ' live</span>');
    else if (ws.state === 'starting') bits.push('<span class="live">starting</span>');
    else if (ws.state === 'ready') bits.push('<span class="live">online · no session</span>');
    else if (ws.state === 'blocked') bits.push('<span class="warn">will not start</span>');
    else if (ws.state === 'disabled') bits.push('<span class="warn">disabled after repeated failures</span>');
    // What the daemon knew all along and the phone never showed.
    const waiting = shortWait(ws.topSession && ws.topSession.waitingFor);
    if (waiting) bits.push('<span class="warn">waiting: ' + esc(waiting) + '</span>');
    if (ws.running && ws.startedAt) bits.push('<span>up ' + esc(fmtSpan(Date.now() - ws.startedAt)) + '</span>');
    if (ws.running && ws.restarts > 0) {
      bits.push('<span>' + ws.restarts + ' restart' + (ws.restarts > 1 ? 's' : '') + '</span>');
    }
    if (ws.profile) bits.push('<span>' + esc(ws.profile) + ' account</span>');
    const e = ws.lastEvent;
    if (e && !(ws.running && e.kind === 'started')) {
      const what = e.kind === 'exited' ? 'exited' + (e.cause ? ' ' + esc(e.cause) : '')
        : e.kind === 'attention' ? 'needs you'
        : esc(e.kind);
      bits.push('<span class="why">' + what + ' ' + esc(fmtWhen(e.at)) + '</span>');
    }
    if (!bits.length) return null;
    const el = document.createElement('div');
    el.className = 'meta';
    el.innerHTML = bits.join('');
    return el;
  }

    function usageLine(ws) {
    if (!ws.usage || !ws.usage.week) return null;
    const sum = u => (u.input || 0) + (u.output || 0) + (u.cacheRead || 0) + (u.cacheWrite || 0);
    const week = sum(ws.usage.week);
    if (!week) return null;
    const el = document.createElement('div');
    el.className = 'usage';
    el.title = 'tokens today / this week';
    el.textContent = fmtTokens(sum(ws.usage.today)) + ' today \u00b7 ' + fmtTokens(week) + ' this week';
    return el;
  }

  // The start options are built even when collapsed: Start reads them, so a
  // profile chosen before opening the panel still applies.
  function startOptions(ws) {
    if (ws.running) return null;
    const box = document.createElement('div');
    box.className = 'startbox';
    if ((ws.profiles || []).length) {
      const sel = document.createElement('select');
      sel.dataset.role = 'profile';
      const none = document.createElement('option');
      none.value = ''; none.textContent = 'default account';
      sel.appendChild(none);
      for (const p of ws.profiles) {
        const o = document.createElement('option');
        o.value = p; o.textContent = p;
        sel.appendChild(o);
      }
      box.appendChild(sel);
    }
    const name = document.createElement('input');
    name.type = 'text'; name.placeholder = 'session name (optional)'; name.dataset.role = 'name';
    name.maxLength = 60;
    box.appendChild(name);
    return box;
  }

  // The branch a session here would start on, with a * for uncommitted work —
  // the answer to "which of these two checkouts am I looking at?". Clamped,
  // because a branch name has no upper bound and the path is sharing the line.
  function branchSuffix(ws) {
    if (!ws.branch) return '';
    return ' \u00b7 ' + clamp(ws.branch, 24) + (ws.dirty ? '*' : '');
  }

  function clamp(text, max) {
    const s = String(text || '');
    return s.length > max ? s.slice(0, max - 1) + '\u2026' : s;
  }

  // tight drops another segment: when the branch is sharing this line, the
  // repo directory identifies the checkout and its parents do not.
  function shortPath(p, tight) {
    const parts = String(p || '').split('/').filter(Boolean);
    const keep = tight ? 1 : 2;
    return parts.length > keep ? '…/' + parts.slice(-keep).join('/') : p;
  }

  async function toggleSessions(ws, btn, box) {
    if (box.style.display !== 'none') { box.style.display = 'none'; btn.textContent = 'sessions \u2304'; return; }
    box.style.display = ''; btn.textContent = 'sessions \u2303';
    box.innerHTML = '';
    const group = (label) => {
      const el = document.createElement('div');
      el.className = 'grp';
      el.innerHTML = '<span>' + esc(label) + '</span><i></i>';
      box.appendChild(el);
    };
    const renderLink = (url, o) => {
      const el = document.createElement('a');
      el.className = o.past ? 's past' : 's';
      el.href = url; el.target = '_blank'; el.rel = 'noopener noreferrer';
      const m = openMode(ws.id);
      if (m === 'browser') el.onclick = (e) => { e.preventDefault(); window.open(url, '_blank', 'noopener'); };
      if (m === 'chrome') el.onclick = (e) => { e.preventDefault(); location.href = chromeUrl(url); };
      const tag = o.bridge ? '<span class="tag" title="Hand-started on the laptop \u2014 its web page may look empty">bridge</span>' : '';
      const text = o.label ? esc(o.label) : esc(url.split('/').pop().slice(0, 14)) + '\u2026';
      const dot = o.past ? '' : '<i class="sdot"></i>';
      el.innerHTML = '<span>' + dot + '<span class="tname">' + text + '</span>' + tag + '</span>' +
        '<span class="when">' + esc(o.when || '') + (o.past ? 'reopen' : 'open \u2197') + '</span>';
      box.appendChild(el);
    };
    const renderLocal = (label, when, sess) => {
      const el = document.createElement('div');
      el.className = 's';
      el.innerHTML = '<span><i class="sdot"></i><span class="tname">' + esc(label) +
        '</span></span><span class="when">' + esc(when) + ' \u00b7 local only</span>';
      // corgi can type /remote-control into the session itself, which is the
      // whole of what makes it reachable from here. Only while it is idle:
      // typing into a session mid-turn lands in the middle of its own work.
      if (sess && sess.id && (sess.status === 'done' || sess.status === 'stale')) {
        const link = document.createElement('button');
        link.className = 'linkme';
        link.textContent = 'Link';
        link.title = 'Type /remote-control in this session so it can be opened from here';
        link.onclick = async (e) => {
          e.stopPropagation();
          link.disabled = true; link.textContent = 'Linking…';
          try {
            const r = await fetch('/launch/send', { method: 'POST', headers: auth,
              body: JSON.stringify({ session: sess.id, text: '/remote-control' }) });
            const j = await r.json().catch(() => ({}));
            if (!r.ok) { toast(j.error || 'could not reach that session', true); link.disabled = false; link.textContent = 'Link'; return; }
            toast('asked ' + esc(label) + ' for a web link — it appears in a moment');
            setTimeout(load, 6000);
          } catch { toast('no connection', true); link.disabled = false; link.textContent = 'Link'; }
        };
        el.querySelector('.when').appendChild(link);
      }
      box.appendChild(el);
    };
    const note = (text) => {
      const el = document.createElement('div');
      el.className = 'hint'; el.textContent = text;
      box.appendChild(el);
    };
    const info = document.createElement('div');
    info.className = 'none'; info.textContent = 'Loading\u2026';
    box.appendChild(info);
    try {
      const r = await fetch('/launch/sessions?workspace=' + encodeURIComponent(ws.id), { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      info.remove();

      const running = j.sessions || [];
      const bridges = (j.links || []).filter(safeClaudeUrl);
      const evs = j.events || [];
      const alive = new Set(bridges);
      for (const sess of running) if (sess.url && safeClaudeUrl(sess.url)) alive.add(sess.url);

      const seen = new Set();
      const older = [];
      const addOlder = (url, when) => {
        if (!safeClaudeUrl(url) || alive.has(url) || seen.has(url)) return;
        seen.add(url); older.push({ url: url, when: when });
      };
      for (const h of (j.history || [])) if (h) addOlder(h.url, fmtWhen(h.at) + ' \u00b7 ');
      for (const url of (ws.sessionLinks || []).slice().reverse()) addOlder(url, '');

      let localOnly = 0;
      const showB = showBridges();
      const liveCount = running.length + (showB ? bridges.filter(u => !running.some(s => s.url === u)).length : 0);
      if (liveCount) group('live now');
      for (const sess of running) {
        const where = whereLabel(sess);
        const waiting = sessionWaiting(sess);
        const when = where + ' \u00b7 ' + fmtWhen(sess.startedAt) +
          (waiting ? ' \u00b7 waiting: ' + waiting : '');
        if (sess.url && safeClaudeUrl(sess.url) && !seen.has(sess.url)) {
          seen.add(sess.url);
          renderLink(sess.url, {
            label: shortSessionName(sess.name, ws.id),
            when: [when, nameNote(sess)].filter(Boolean).join(' \u00b7 ') + ' \u00b7 ',
          });
          continue;
        }
        localOnly++;
        renderLocal(shortSessionName(sess.name, ws.id) || 'session', when, sess);
      }
      let bridgeRows = 0;
      let bridgeHidden = 0;
      for (const url of bridges) {
        if (seen.has(url)) continue;
        if (!showB) { bridgeHidden++; continue; }
        seen.add(url);
        bridgeRows++;
        renderLink(url, { bridge: true });
      }
      if (localOnly) note('local only = running on the laptop with no web link yet. Link asks the session for one; a busy session has to be asked by hand with /remote-control.');
      if (bridgeRows) note('bridge = started by hand on the laptop; its page shows only what you send from it.');
      if (bridgeHidden) note(bridgeHidden + ' bridge session' + (bridgeHidden > 1 ? 's' : '') + ' hidden \u2014 enable in Settings.');

      if (older.length) {
        group('earlier \u00b7 not running');
        for (const row of older) renderLink(row.url, { past: true, when: row.when });
      }
      if (!liveCount && !older.length && !evs.length) {
        const none = document.createElement('div');
        none.className = 'none'; none.textContent = 'No sessions yet for this workspace.';
        box.appendChild(none);
        return;
      }
      if (evs.length) group('activity');
      for (const ev of evs) {
        const el = document.createElement('div');
        el.className = 'evrow';
        const what = ev.kind + (ev.cause ? ' \u00b7 ' + ev.cause : '') + (ev.reason ? ' \u2014 ' + ev.reason : '');
        el.innerHTML = '<b>' + esc(what.slice(0, 90)) + '</b><span>' + esc(fmtWhen(ev.at)) + '</span>';
        box.appendChild(el);
      }
      renderWorkspaceFacts(ws, box, group);
    } catch (e) { info.textContent = '\u2717 ' + e.message; }
  }

  // What the daemon knows about this workspace and the card has no room for.
  // corgi agent status on the laptop always showed these; the phone did not.
  function workspaceFacts(ws) {
    const facts = [];
    if (ws.branch) facts.push(['checkout', ws.branch + (ws.dirty ? ' \u00b7 uncommitted work' : ' \u00b7 clean')]);
    if (ws.profile) facts.push(['account', ws.profile]);
    if (ws.running && ws.startedAt) {
      const how = ws.origin === 'remote' ? ' \u00b7 started from a phone'
        : ws.origin === 'auto' ? ' \u00b7 started with the daemon' : '';
      facts.push(['supervised', 'for ' + fmtSpan(Date.now() - ws.startedAt) + how]);
    }
    if (ws.restarts > 0) facts.push(['restarts', String(ws.restarts) + (ws.lastCause ? ' \u00b7 last ' + ws.lastCause : '')]);
    else if (ws.lastCause) facts.push(['last exit', ws.lastCause]);
    if (ws.wakeLock) facts.push(['wake lock', 'the machine is held awake while this runs']);
    if (ws.deviceOnly) facts.push(['device', 'online with no session — Start opens one here, or create one from the Claude app’s device list']);
    if (ws.remark) facts.push(['note', ws.remark]);
    if (ws.pid) facts.push(['pid', String(ws.pid)]);
    if (ws.disabled) facts.push(['disabled', 'the daemon stopped retrying this one \u2014 fix the cause, then Start']);
    return facts;
  }

  function renderWorkspaceFacts(ws, box, group) {
    const facts = workspaceFacts(ws);
    if (!facts.length) return;
    group('this workspace');
    for (const [label, value] of facts) {
      const el = document.createElement('div');
      el.className = 'kv';
      el.innerHTML = '<b>' + esc(label) + '</b><span>' + esc(value) + '</span>';
      box.appendChild(el);
    }
  }

  // Claude Code names what a session is blocked on when it knows; absent means
  // it did not say, not that the session is idle.
  function sessionWaiting(sess) {
    if (sess.waitingFor) return sess.waitingFor;
    if (sess.status === 'waiting' || sess.tempo === 'blocked') return 'your answer';
    return '';
  }

  function whereLabel(sess) {
    const ep = String(sess.entrypoint || '');
    if (ep.indexOf('vscode') >= 0) return 'vscode';
    if (ep === 'sdk-cli') return 'remote';
    return sess.kind === 'interactive' ? 'terminal' : (sess.kind || 'session');
  }

  function fmtTokens(n) {
    // Rounding is applied before the threshold is chosen: 999,500 is 1.0M, not
    // "1000k", and 999,999,500 is 1.0B.
    if (Math.round(n / 1e8) >= 10) return (n / 1e9).toFixed(1) + 'B';
    if (Math.round(n / 1e5) >= 10) return (n / 1e6).toFixed(1) + 'M';
    if (Math.round(n / 1e3) >= 1) return Math.round(n / 1e3) + 'k';
    return String(n || 0);
  }

  // "choose: allow or deny the edit" is written for a dialog box; the card has
  // room for the ask, not the sentence.
  function shortWait(text) {
    if (!text) return '';
    return clamp(String(text).replace(/^choose:\s*/i, ''), 28);
  }

  function fmtSpan(ms) {
    const mins = Math.floor(ms / 60000);
    if (mins < 1) return 'a moment';
    if (mins < 60) return mins + 'm';
    const hours = Math.floor(mins / 60);
    if (hours < 24) return hours + 'h';
    return Math.floor(hours / 24) + 'd';
  }

  function fmtWhen(ms) {
    if (!ms) return '';
    const diff = (Date.now() - ms) / 1000;
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    return new Date(ms).toLocaleDateString();
  }

  // Only ever derived from a URL that already passed safeClaudeUrl, so the
  // scheme swap cannot smuggle an arbitrary scheme. googlechromes:// is iOS
  // Chrome's https handler — it forces Chrome even when this page runs in
  // Safari or the Claude app's webview.
  const chromeUrl = u => u.replace(/^https:\/\//, 'googlechromes://');

  // app mode uses a real anchor tap so iOS deep-links into the Claude app;
  // browser mode opens via JS, which keeps the session in this browser; chrome
  // mode forces Chrome via its URL scheme (right for a workspace signed into a
  // different Claude account than the app — e.g. work vs personal).
  // Where a session link opens is a per-workspace choice, so every session
  // link has to honour it — not only the one on the Stacks card, which is
  // where the setting happens to live.
  function sessionOpener(workspaceId, url, label, cls) {
    const mode = openMode(workspaceId);
    if (mode === 'browser' || mode === 'chrome') {
      const b = document.createElement('button');
      b.className = cls; b.textContent = label;
      b.onclick = () => {
        if (mode === 'chrome') { location.href = chromeUrl(url); }
        else { window.open(url, '_blank', 'noopener'); }
      };
      return b;
    }
    const a = document.createElement('a');
    a.className = cls; a.href = url; a.textContent = label;
    a.target = '_blank'; a.rel = 'noopener noreferrer';
    return a;
  }

  function openControl(ws) {
    return sessionOpener(ws.id, ws.sessionUrl, 'Open', 'open');
  }

  function modeSwitch(id) {
    const order = ['app', 'browser', 'chrome'];
    const cur = openMode(id);
    const b = document.createElement('button');
    b.className = 'chip';
    b.title = 'Where session links open — tap to change';
    b.innerHTML = 'open in <b>' + esc(cur) + '</b> ▾';
    b.onclick = () => { setOpenMode(id, order[(order.indexOf(cur) + 1) % order.length]); render(lastWorkspaces); };
    return b;
  }

  function stopControl(id) {
    const b = document.createElement('button');
    b.className = 'chip danger'; b.textContent = 'Stop';
    b.onclick = async () => {
      b.disabled = true; b.textContent = 'Stopping…';
      try {
        const r = await fetch('/launch/stop', { method: 'POST', headers: auth,
          body: JSON.stringify({ workspace: id }) });
        const j = await r.json();
        if (!r.ok) throw new Error(j.error || r.status);
        toast('Stopping ' + id + '…');
        setTimeout(load, 1200);
      } catch (e) {
        b.disabled = false; b.textContent = 'Stop';
        toast(e.message, true);
      }
    };
    return b;
  }

  async function startSession(id, btn) {
    btn.disabled = true; btn.textContent = 'Starting…';
    const box = btn.closest('.ws').querySelector('.startbox');
    const pick = (role) => {
      const el = box && box.querySelector('[data-role="' + role + '"]');
      return el ? el.value.trim() : '';
    };
    try {
      const r = await fetch('/launch/start', { method: 'POST', headers: auth,
        body: JSON.stringify({ workspace: id, profile: pick('profile'), name: pick('name') }) });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      toast('Starting ' + id + '…');
      // Show it as starting straight away: a card still offering Start invites
      // a second one before the daemon has answered the first.
      const ws = lastWorkspaces.find(w => w.id === id);
      if (ws) { ws.running = true; ws.state = 'starting'; ws.note = ''; render(lastWorkspaces); }
      poll(id, 0);
    } catch (e) {
      btn.disabled = false; btn.textContent = 'Retry';
      toast(e.message, true);
    }
  }

  async function poll(id, n) {
    if (n > 30) { load(); return; } // give up on the link after ~30s; the list still refreshes
    try {
      const r = await fetch('/launch/workspaces', { headers: auth });
      const j = await r.json();
      const ws = (j.workspaces || []).find(w => w.id === id);
      // Got the link, or the daemon reported why it won't start — either way, stop
      // polling and re-render so the reason (or the Open button) shows.
      if (ws && (ws.sessionUrl || (!ws.running && ws.note))) {
        render(j.workspaces);
        if (ws.note && !ws.running) toast(ws.note, true);
        return;
      }
    } catch {}
    setTimeout(() => poll(id, n + 1), 1000);
  }
</script>
`

// The phone can answer a permission prompt and type into a session, the
// same two things a deck key does. A risky Bash prompt is refused here, so
// the person hears why on the phone rather than finding a notice later.

func launchAnswerHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, answer} to answer a prompt")
		return
	}
	var req struct {
		Session string `json:"session"`
		Answer  string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the answer")
		return
	}
	answer := strings.ToLower(strings.TrimSpace(req.Answer))
	if answer != "allow" && answer != "always" && answer != "deny" {
		writeLaunchError(w, http.StatusBadRequest, "answer is allow, always or deny")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if session.Status != sessions.StatusNeedsInput || session.Pending == nil {
		writeLaunchError(w, http.StatusConflict, "nothing is waiting for an answer there")
		return
	}
	if session.Pending.Risky() && answer != "deny" {
		writeLaunchError(w, http.StatusForbidden, "that command is one to look at first: answer it on the laptop")
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionAnswer, SessionID: session.ID, Answer: answer, Source: "phone"})
}

// launchFreshHandler restarts a drifting session clean, under the same
// account, from a handoff: `corgi agent carry <id> --fresh` run as itself,
// so the phone and the CLI do exactly the same thing.
func launchFreshHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session} to restart it clean from a handoff")
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "corgi"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "agent", "carry", session.ID, "--fresh", "--json").CombinedOutput()
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, firstLineOf(strings.TrimSpace(string(out))+" "+err.Error()))
		return
	}
	writeLaunchJSON(w, map[string]any{"done": "restarting " + firstNonEmpty(session.Display, session.Label) + " clean, from a handoff"})
}

func launchSendHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, text} to type into a session")
		return
	}
	var req struct {
		Session string `json:"session"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the text")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeLaunchError(w, http.StatusBadRequest, "nothing to type")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if session.Status == sessions.StatusGone {
		writeLaunchError(w, http.StatusConflict, "that session is closed")
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionSend, SessionID: session.ID, Text: text, Enter: true, Source: "phone"})
}

// launchSessionFor finds a board session by id or display name. A non-zero
// code is the HTTP status to answer with.
func launchSessionFor(ref string) (sessions.Session, int, string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return sessions.Session{}, http.StatusBadRequest, "a session is required"
	}
	dir, err := agentDir()
	if err != nil {
		return sessions.Session{}, http.StatusInternalServerError, err.Error()
	}
	rep, err := readBoard(dir)
	if err != nil {
		return sessions.Session{}, http.StatusInternalServerError, err.Error()
	}
	for _, s := range rep.Sessions {
		if s.ID == ref || s.Display == ref {
			return s, 0, ""
		}
	}
	return sessions.Session{}, http.StatusNotFound, "no such session on the board"
}

func launchBoardCommand(w http.ResponseWriter, c command.Command) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		writeLaunchError(w, http.StatusServiceUnavailable, "the corgi daemon is not running on that machine")
		return
	}
	if _, err := command.Write(dir, c); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	daemon.Nudge(info)
	writeLaunchJSON(w, map[string]any{"ok": true, "action": c.Action})
}

// A new chat from the phone: a window, a prompt, a model, a profile. The
// shell line the editor runs carries this binary and validated flags only;
// the prompt goes by id (see savePrompt) and is read once by the new
// session.

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func launchNewHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {window, prompt, model, profile} to open a chat")
		return
	}
	var req struct {
		Window    string `json:"window"`
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		Profile   string `json:"profile"`
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model != "" && !validModel(model) {
		writeLaunchError(w, http.StatusBadRequest, "model: letters, digits, dots and dashes only")
		return
	}
	profile := strings.TrimSpace(req.Profile)
	if profile != "" && profile != "default" {
		if !profileNamePattern.MatchString(profile) || !containsString(launchProfileNames(), profile) {
			writeLaunchError(w, http.StatusBadRequest, "no such profile")
			return
		}
	} else {
		profile = ""
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	window := strings.TrimSpace(req.Window)
	if window != "" {
		rep, err := readBoard(dir)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		known := false
		for _, win := range rep.Windows {
			if win.ID == window {
				known = true
				break
			}
		}
		if !known {
			writeLaunchError(w, http.StatusNotFound, "that editor window is not connected any more")
			return
		}
	}
	args := []string{}
	if ws := strings.TrimSpace(req.Workspace); ws != "" {
		if _, err := workspaceRoot(ws); err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		args = append(args, "--workspace", ws)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		id, err := savePrompt(dir, prompt)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		args = append(args, "--prompt-id", id)
	}
	launchBoardCommand(w, command.Command{Action: command.ActionNew, WindowID: window, Command: daemon.NewSessionCommand(args...), Source: "phone"})
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// watchBatchMax is how many issues one session is asked to ship at once.
const watchBatchMax = 10

// launchEventsHandler lists what the watch has seen, newest first, so the
// phone can show the tracker issues and reviews waiting for a decision.
// launchRunHandler is one unattended run's log tail and record, for the
// phone: what it did, what it left, what it cost.
func launchRunHandler(w http.ResponseWriter, r *http.Request) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeLaunchError(w, http.StatusBadRequest, "key is required")
		return
	}
	tail := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && n > 0 && n <= 2000 {
		tail = n
	}
	out := map[string]any{"key": key}
	for _, rec := range watch.LoadFixLog(dir).RecentFixes("", 200) {
		if rec.Key != key {
			continue
		}
		out["ref"] = firstNonEmptyString(rec.Ref, rec.Key)
		out["workspace"] = rec.Workspace
		out["running"] = !rec.Done()
		out["startedAt"] = rec.StartedAt
		out["outcome"] = rec.Outcome()
		if len(rec.PRs) > 0 {
			out["prs"] = rec.PRs
		}
		if rec.Handover != "" {
			out["handover"] = rec.Handover
		}
		if rec.SpentPercent > 0 {
			out["spentPercent"] = rec.SpentPercent
		}
		if rec.Tokens > 0 {
			out["tokens"], out["costUSD"] = rec.Tokens, rec.CostUSD
		}
		if rec.Branch != "" {
			out["branch"] = rec.Branch
		}
		break
	}
	log := daemon.RunLog(dir, key, tail)
	if _, known := out["ref"]; !known && log == "" {
		writeLaunchError(w, http.StatusNotFound, "no run for that key")
		return
	}
	out["log"] = log
	writeLaunchJSON(w, out)
}

// launchKanbanHandler is the derived board: one card per ticket with its
// column and why, plus the columns each tracker can move a ticket to.
func launchKanbanHandler(w http.ResponseWriter, r *http.Request) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards := gatherKanban(dir, strings.TrimSpace(r.URL.Query().Get("workspace")), time.Now())
	boards := map[string]any{}
	cache := watch.LoadBoardCache(dir)
	for _, c := range cards {
		if c.Workspace == "" {
			continue
		}
		if _, seen := boards[c.Workspace]; seen {
			continue
		}
		if info := cache.Get(c.Workspace); info.Has() {
			names := make([]string, 0, len(info.Statuses))
			for _, st := range info.Statuses {
				names = append(names, st.Name)
			}
			boards[c.Workspace] = map[string]any{"columns": names, "me": info.Me.Name}
		}
	}
	writeLaunchJSON(w, map[string]any{"columns": kanbanColumns, "cards": cards, "boards": boards})
}

func launchEventsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET to list watch events")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type row struct {
		Key        string    `json:"key"`
		Kind       string    `json:"kind"`
		Ref        string    `json:"ref"`
		Title      string    `json:"title"`
		URL        string    `json:"url,omitempty"`
		Workspace  string    `json:"workspace,omitempty"`
		At         time.Time `json:"at"`
		Actionable bool      `json:"actionable"`
		State      string    `json:"state,omitempty"`
		// Blocked is why unattended runs leave this ticket alone, when they do.
		Blocked   string `json:"blocked,omitempty"`
		BlockedBy string `json:"blockedBy,omitempty"`
		// Priority is 0 urgent, 1 high, 2 the rest, from the ticket's labels.
		Priority int `json:"priority"`
	}
	out := []row{}
	// The events log keeps the column a ticket arrived in. A move made since
	// then is the truth, so it wins.
	moved := watch.LoadStateLog(dir)
	// The inbox is what is still waiting. Seen is not the test — every
	// delivered event is seen — so it is the dismissed ones that leave.
	state := watch.LoadState(dir)
	fixLog := watch.LoadFixLog(dir)
	keeper := watch.NewInboxKeeper(time.Now())
	for _, e := range watch.RecentEvents(dir, 40) {
		if state.IsIgnored(e.Key) || !keeper.Keep(e) {
			continue
		}
		// Merged, closed, done: the row is history, not work. The daemon
		// refreshes these each round, so a merge request merged an hour after
		// it was recorded stops being listed without anyone dismissing it.
		current := e.State
		if now, ok := moved.Get(e.Key); ok {
			current = now.Status
		}
		if watch.Settled(e, current) != "" {
			continue
		}
		r := row{Key: e.Key, Kind: string(e.Kind), Ref: e.Ref, Title: firstLineOf(e.Title),
			URL: e.URL, Workspace: e.Workspace, At: e.At, Actionable: daemon.FixPrompt(e) != "", State: current, Priority: watch.Priority(e)}
		if b, ok := fixLog.Blocked(e.Workspace, e.Ref); ok {
			r.Blocked, r.BlockedBy = b.Reason, b.By
		}
		out = append(out, r)
	}
	fixes := []map[string]any{}
	for _, r := range fixLog.RecentFixes("", 25) {
		row := map[string]any{"key": r.Key, "ref": firstNonEmptyString(r.Ref, r.Key), "workspace": r.Workspace,
			"running": !r.Done(), "startedAt": r.StartedAt, "outcome": r.Outcome()}
		if len(r.PRs) > 0 {
			row["prs"] = r.PRs
		}
		if r.Error != "" {
			row["error"] = firstLineOf(r.Error)
		}
		fixes = append(fixes, row)
	}
	// The columns each workspace can move a ticket to, read from the cache
	// so a menu on the phone draws without a round trip to the tracker.
	boards := map[string]any{}
	cache := watch.LoadBoardCache(dir)
	seen := map[string]bool{}
	for _, e := range out {
		if e.Workspace == "" || seen[e.Workspace] {
			continue
		}
		seen[e.Workspace] = true
		if info := cache.Get(e.Workspace); info.Has() {
			names := make([]string, 0, len(info.Statuses))
			for _, st := range info.Statuses {
				names = append(names, st.Name)
			}
			boards[e.Workspace] = map[string]any{"columns": names, "me": info.Me.Name}
		}
	}
	// One queue, not one per repo. A review someone is waiting on outranks
	// your own backlog ticket whatever repo it came from, and within a rank
	// the one that has waited longest goes first.
	sort.SliceStable(out, func(i, j int) bool {
		if pi, pj := out[i].Priority, out[j].Priority; pi != pj {
			return pi < pj
		}
		ri, rj := waitingRank(out[i].Kind), waitingRank(out[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return out[i].At.Before(out[j].At)
	})
	writeLaunchJSON(w, map[string]any{"events": out, "fixes": fixes, "boards": boards})
}

// launchTicketHandler is the phone changing one ticket: move it to a column,
// assign it, or drop it out of the inbox. Each is something someone tapped;
// nothing here happens on a poll.
func launchTicketHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {key, do, status} to change a ticket")
		return
	}
	var req struct {
		Key    string `json:"key"`
		Do     string `json:"do"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	event, ok := watch.FindEvent(dir, strings.TrimSpace(req.Key))
	if !ok {
		writeLaunchError(w, http.StatusNotFound, "no such watch event")
		return
	}
	// Ignoring is ours alone: it takes the row out of the inbox and stops the
	// unattended mode picking it up, and writes nothing to anyone's tracker.
	if strings.TrimSpace(req.Do) == "ignore" {
		_ = watch.LoadState(dir).Ignore(event.Key)
		writeLaunchJSON(w, map[string]any{"done": "ignored " + firstNonEmptyString(event.Ref, event.Key)})
		return
	}
	// Unblocking is a person's call too: it lets the unattended mode back on
	// the ticket and clears the reason from the workpad.
	if strings.TrimSpace(req.Do) == "unblock" {
		if !watch.LoadFixLog(dir).Unblock(event.Workspace, event.Ref) {
			writeLaunchError(w, http.StatusBadRequest, event.Ref+" is not blocked")
			return
		}
		if _, _, err := watchWriter(dir, event.Workspace); err == nil {
			go writeWorkpad(dir, event.Workspace, event.Ref, "Blocked", "")
		}
		writeLaunchJSON(w, map[string]any{"done": "unblocked " + event.Ref})
		return
	}
	if event.Workspace == "" {
		writeLaunchError(w, http.StatusBadRequest, "that event belongs to no workspace")
		return
	}
	writer, _, err := watchWriter(dir, event.Workspace)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ref := strings.TrimSpace(event.Ref)
	switch strings.TrimSpace(req.Do) {
	case "move":
		status := strings.TrimSpace(req.Status)
		if status == "" {
			writeLaunchError(w, http.StatusBadRequest, "name the column to move it to")
			return
		}
		if err := writer.Move(ctx, ref, status); err != nil {
			writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
			return
		}
		_ = watch.LoadStateLog(dir).SetFrom(event.Key, status, event.State, time.Now())
		writeLaunchJSON(w, map[string]any{"done": ref + " → " + status, "state": status})
	case "merge", "close":
		// Only what corgi opened: this is for finishing its own work, not a
		// button that can close anything a link points at.
		link := prCorgiOpened(dir, event.Key)
		if link == "" {
			writeLaunchError(w, http.StatusBadRequest, "corgi did not open a pull request for this")
			return
		}
		secrets := watch.LoadSecretsFor(dir, event.Workspace)
		var err error
		if strings.TrimSpace(req.Do) == "merge" {
			err = watch.MergePR(ctx, secrets, link)
		} else {
			err = watch.ClosePR(ctx, secrets, link)
		}
		if err != nil {
			writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
			return
		}
		writeLaunchJSON(w, map[string]any{"done": strings.TrimSpace(req.Do) + "d " + link, "url": link})
	case "assign":
		me := watch.LoadBoardCache(dir).Get(event.Workspace).Me
		if me.ID == "" {
			got, err := writer.Whoami(ctx)
			if err != nil {
				writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
				return
			}
			me = got
		}
		if err := writer.Assign(ctx, ref, me.ID); err != nil {
			writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
			return
		}
		writeLaunchJSON(w, map[string]any{"done": ref + " is yours"})
	default:
		writeLaunchError(w, http.StatusBadRequest, "do is move, assign, merge, close, ignore or unblock")
	}
}

// waitingRank orders the inbox by who is stuck. Someone waiting on you comes
// first, then a build nobody can merge past, then a conversation, then work
// that is only waiting on you starting it.
func waitingRank(kind string) int {
	switch kind {
	case "review.requested":
		return 0 // a person is blocked on you
	case "ci.failed":
		return 1 // the branch is blocked
	case "pr.review", "pr.comment":
		return 2 // your own PR, someone replied
	case "issue.comment":
		return 3
	default:
		return 4 // a fresh ticket blocks nobody yet
	}
}

// prCorgiOpened is the pull request corgi's own run opened for this event,
// or "" when it opened none. The merge button never acts on a link that did
// not come from a run corgi made.
func prCorgiOpened(agentD, key string) string {
	for _, r := range watch.LoadFixLog(agentD).RecentFixes("", 50) {
		if r.Key == key && len(r.PRs) > 0 {
			return r.PRs[0]
		}
	}
	return ""
}

// launchWorkOnHandler hands one watch event to a real session: the same
// prompt the daemon's own fix would have used, opened as a chat someone can
// watch and steer. The prompt travels by id, never on the command line.
func launchWorkOnHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {key, window, model, profile} to work on a watch event")
		return
	}
	var req struct {
		Key     string   `json:"key"`
		Keys    []string `json:"keys"`
		Window  string   `json:"window"`
		Model   string   `json:"model"`
		Profile string   `json:"profile"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	keys := req.Keys
	if len(keys) == 0 && strings.TrimSpace(req.Key) != "" {
		keys = []string{req.Key}
	}
	if len(keys) == 0 {
		writeLaunchError(w, http.StatusBadRequest, "name the event to work on")
		return
	}
	if len(keys) > watchBatchMax {
		writeLaunchError(w, http.StatusBadRequest, "too many at once")
		return
	}
	var events []watch.Event
	for _, key := range keys {
		event, ok := watch.FindEvent(dir, strings.TrimSpace(key))
		if !ok {
			writeLaunchError(w, http.StatusNotFound, "no such watch event")
			return
		}
		events = append(events, event)
	}
	// A batch is one workspace's issues: the skill specs them together
	// against that checkout, and two checkouts have nothing to share.
	for _, e := range events[1:] {
		if e.Workspace != events[0].Workspace {
			writeLaunchError(w, http.StatusBadRequest, "those are in different workspaces — take one workspace at a time")
			return
		}
	}
	prompt := daemon.BatchPrompt(events)
	if prompt == "" {
		writeLaunchError(w, http.StatusConflict, "only new issues can be worked on together")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model != "" && !validModel(model) {
		writeLaunchError(w, http.StatusBadRequest, "model: letters, digits, dots and dashes only")
		return
	}
	profile := strings.TrimSpace(req.Profile)
	if profile != "" && profile != "default" {
		if !profileNamePattern.MatchString(profile) || !containsString(launchProfileNames(), profile) {
			writeLaunchError(w, http.StatusBadRequest, "no such profile")
			return
		}
	} else {
		profile = ""
	}
	window := strings.TrimSpace(req.Window)
	rep, boardErr := readBoard(dir)
	if window != "" {
		if boardErr != nil {
			writeLaunchError(w, http.StatusInternalServerError, boardErr.Error())
			return
		}
		if !windowConnected(rep, window) {
			writeLaunchError(w, http.StatusNotFound, "that editor window is not connected any more")
			return
		}
	}
	// A ticket belongs in its own checkout. When an editor is already open on
	// that workspace, the session opens there rather than wherever the phone
	// happened to point; with none open, the caller's choice stands.
	if boardErr == nil {
		if own := windowOnWorkspace(rep, dir, events[0].Workspace); own != "" {
			window = own
		}
	}
	id, err := savePrompt(dir, prompt)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The event's workspace, not the terminal's: a phone has no cwd, so
	// without this the session opens in whichever checkout the editor window
	// was in — the wrong repo, under the wrong account.
	// Picking a story up is a board move as much as a session: the column
	// says someone has it. Off unless the workspace names a pickup status,
	// and never allowed to hold up the session it belongs to.
	go markPickedUp(dir, events)
	args := []string{}
	if ws := strings.TrimSpace(events[0].Workspace); ws != "" {
		args = append(args, "--workspace", ws)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--prompt-id", id)
	launchBoardCommand(w, command.Command{Action: command.ActionNew, WindowID: window,
		Command: daemon.NewSessionCommand(args...), Source: "phone"})
}

// windowOnWorkspace is a connected editor window whose folder is inside the
// workspace's checkout, or "" when none is.
func windowOnWorkspace(rep boardReport, agentD, workspaceID string) string {
	if strings.TrimSpace(workspaceID) == "" {
		return ""
	}
	root, err := workspaceRoot(workspaceID)
	if err != nil {
		return ""
	}
	best, bestFocus := "", time.Time{}
	for _, win := range rep.Windows {
		for _, folder := range win.Folders {
			if !underRoot(folder, root) {
				continue
			}
			// Several windows on one repo: the one most recently in front.
			if best == "" || win.FocusedAt.After(bestFocus) {
				best, bestFocus = win.ID, win.FocusedAt
			}
			break
		}
	}
	return best
}

// underRoot says a folder is the workspace's checkout or inside it. Both
// forms of each path are compared, because EvalSymlinks only resolves what
// exists — on macOS a temp root resolves to /private/var while a subfolder
// that is not there yet does not, and the two would never match.
func underRoot(folder, root string) bool {
	for _, r := range pathForms(root) {
		for _, f := range pathForms(folder) {
			if f != "" && r != "" && (f == r || strings.HasPrefix(f, r+string(filepath.Separator))) {
				return true
			}
		}
	}
	return false
}

func pathForms(p string) []string {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	abs, err := filepath.Abs(expandTilde(p))
	if err != nil {
		return nil
	}
	forms := []string{filepath.Clean(abs)}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		if clean := filepath.Clean(real); clean != forms[0] {
			forms = append(forms, clean)
		}
	}
	return forms
}

func windowConnected(rep boardReport, window string) bool {
	for _, win := range rep.Windows {
		if win.ID == window {
			return true
		}
	}
	return false
}
