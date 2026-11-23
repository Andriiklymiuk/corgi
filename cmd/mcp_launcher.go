package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/transcript"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

const (
	getOnlyMessage            = "GET only"
	workspaceRequiredMessage  = "a workspace is required"
	requestUnreadableMessage  = "could not read the request"
	agentDirUnresolvedMessage = "could not resolve the agent data directory"
	modelNameRuleMessage      = "model: letters, digits, dots and dashes only"
	noSuchProfileMessage      = "no such profile"
)

type launchWorkspace struct {
	ID           string            `json:"id"`
	Aliases      []string          `json:"aliases,omitempty"`
	Path         string            `json:"path"`
	Status       string            `json:"status"`
	Running      bool              `json:"running"`
	SessionURL   string            `json:"sessionUrl,omitempty"`
	SessionLinks []string          `json:"sessionLinks,omitempty"`
	Note         string            `json:"note,omitempty"`
	State        string            `json:"state"`
	Disabled     bool              `json:"disabled,omitempty"`
	StartedAt    int64             `json:"startedAt,omitempty"`
	Restarts     int               `json:"restarts,omitempty"`
	Profile      string            `json:"profile,omitempty"`
	WakeLock     bool              `json:"wakeLock,omitempty"`
	Origin       string            `json:"origin,omitempty"`
	PID          int               `json:"pid,omitempty"`
	LastCause    string            `json:"lastCause,omitempty"`
	DeviceOnly   bool              `json:"deviceOnly,omitempty"`
	Remark       string            `json:"remark,omitempty"`
	Branch       string            `json:"branch,omitempty"`
	Dirty        bool              `json:"dirty,omitempty"`
	Live         int               `json:"live"`
	TopSession   *launchTopSession `json:"topSession,omitempty"`
	Usage        *usage.Report     `json:"usage,omitempty"`
	LastEvent    *launchLastEvent  `json:"lastEvent,omitempty"`
	Profiles     []string          `json:"profiles,omitempty"`
	Agents       []string          `json:"agents,omitempty"`
}

type launchTopSession struct {
	Name       string `json:"name"`
	NameSource string `json:"nameSource,omitempty"`
	NameSince  int64  `json:"nameSince,omitempty"`
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
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		launchRegisterWorkspace(w, r)
		return
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the workspaces, or POST {path, id} to register one")
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

func launchRegisterWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	path := filepath.Clean(strings.TrimSpace(req.Path))
	if !filepath.IsAbs(path) {
		writeLaunchError(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	id, err := registerWorkspace(path, strings.TrimSpace(req.ID), nil, "", false, false, nil)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"done": "registered " + id, "id": id, "path": path})
}

func launchProfilesHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		profiles, err := loadProfiles(dir)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not read the profiles")
			return
		}
		type row struct {
			Name      string `json:"name"`
			ConfigDir string `json:"configDir,omitempty"`
			Bin       string `json:"bin,omitempty"`
		}
		out := make([]row, 0, len(profiles))
		for _, name := range sortedProfileNames(profiles) {
			out = append(out, row{name, profiles[name].ConfigDir, profiles[name].Bin})
		}
		writeLaunchJSON(w, map[string]any{"profiles": out})
	case http.MethodPost:
		var req struct {
			Name      string `json:"name"`
			ConfigDir string `json:"configDir"`
			Bin       string `json:"bin"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
			return
		}
		name := strings.TrimSpace(req.Name)
		if !profileNamePattern.MatchString(name) {
			writeLaunchError(w, http.StatusBadRequest, "a profile name is letters, digits, - and _")
			return
		}
		if err := addProfile(dir, name, config.WorkspaceConfig{ConfigDir: strings.TrimSpace(req.ConfigDir), Bin: strings.TrimSpace(req.Bin)}); err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLaunchJSON(w, map[string]any{"done": "profile " + name + " saved", "name": name})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the profiles, or POST {name, configDir} to add one")
	}
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

// workspaceAgents is the order a workspace tries, when it is more than claude.
func workspaceAgents(id string) []string {
	user := loadUserConfigQuietly()
	if user == nil {
		return nil
	}
	order := config.Resolve(id, nil, user).AgentOrder()
	if len(order) == 1 && order[0] == harness.Claude {
		return nil
	}
	return order
}

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
		row.Agents = workspaceAgents(ws.ID)
		row.Branch, row.Dirty = workspaceCheckout(ws.ID, ws.AbsPath)
		row.Live, row.TopSession, row.LastEvent = workspaceActivity(ws.ID, ws.AbsPath, s.profile)
		row.Usage = workspaceUsage(ws.ID, ws.AbsPath, s.profile)
		row.State = launchState(row)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

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
	idleDevice := ws.Running && ws.DeviceOnly && ws.SessionsThisRun == 0
	url := ws.SessionURL
	if idleDevice {
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

func launchState(row launchWorkspace) string {
	return launchStateAt(row, time.Now())
}

const launchReadyAfter = 15 * time.Second

func launchStateAt(row launchWorkspace, now time.Time) string {
	switch {
	case row.Disabled:
		return "disabled"
	case row.Note != "" && !row.Running:
		return "blocked"
	case row.LastEvent != nil && row.LastEvent.Kind == "attention":
		return "attention"
	case row.Live > 0:
		return "live"
	case row.Running && row.DeviceOnly && row.StartedAt > 0 &&
		now.Sub(time.UnixMilli(row.StartedAt)) >= launchReadyAfter:
		return "ready"
	case row.Running:
		return "starting"
	}
	return "stopped"
}

func workspaceActivity(id, absPath, profile string) (int, *launchTopSession, *launchLastEvent) {
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

func workspaceUsage(id, absPath, profile string) *usage.Report {
	if absPath == "" {
		return nil
	}
	_, configDir, ok := workspaceSessionTarget(id, profile)
	if !ok {
		return nil
	}

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
		dup := rep
		entry.report = &dup
	}
}

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
		writeLaunchError(w, http.StatusBadRequest, workspaceRequiredMessage)
		return
	}
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
		writeLaunchError(w, http.StatusBadRequest, workspaceRequiredMessage)
		return
	}
	result, err := mcpSessionStop(req.Workspace)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, result)
}

type claudeSession struct {
	Name            string `json:"name"`
	NameSource      string `json:"nameSource,omitempty"`
	NameSince       int64  `json:"nameSince,omitempty"`
	ProcStart       string `json:"procStart,omitempty"`
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

func launchSessionsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, getOnlyMessage)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("workspace"))
	if id == "" {
		writeLaunchError(w, http.StatusBadRequest, workspaceRequiredMessage)
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
		writeLaunchError(w, http.StatusMethodNotAllowed, getOnlyMessage)
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
			if !d.StartedAt.IsZero() {
				info["daemonSince"] = d.StartedAt
			}
		}
		if up := loadUpSettings(dir); up.Provider != "" || up.TunnelHostname != "" {
			info["tunnel"] = map[string]any{"provider": up.Provider, "host": up.TunnelHostname, "stable": strings.TrimSpace(up.TunnelHostname) != ""}
		}
	}
	if latest := cachedLatestVersion(); latest != "" && versionNewer(latest, APP_VERSION) {
		info["latest"] = latest
	}
	writeLaunchJSON(w, info)
}

func launchBoardHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, getOnlyMessage)
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
	fillUnread(rep.Sessions)
	writeLaunchJSON(w, rep)
}

const unreadTail = 60

func fillUnread(list []sessions.Session) {
	for i := range list {
		s := &list[i]
		if s.ReadAt.IsZero() || s.Status == sessions.StatusGone || !streamAllowedFor(s.Label) {
			continue
		}
		s.Unread = unreadOf(*s)
	}
}

func unreadOf(s sessions.Session) *sessions.Unread {
	path := transcriptPathFor(s)
	if path == "" || !transcript.Exists(path) {
		return nil
	}
	entries, _, err := transcript.Last(path, unreadTail)
	if err != nil {
		return nil
	}
	u := sessions.Unread{Since: s.ReadAt}
	for _, e := range entries {
		if e.Kind != "assistant" || !e.At.After(s.ReadAt) {
			continue
		}
		if u.Lines == 0 {
			u.First = truncateLine(firstLineOf(e.Text), 120)
		}
		u.Lines++
	}
	if u.Lines == 0 {
		return nil
	}
	return &u
}

var latestVersion struct {
	mu      sync.Mutex
	value   string
	checked time.Time
}

func versionNewer(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	if len(pa) != 3 || len(pb) != 3 {
		return a != b
	}
	for i := range pa {
		x, errA := strconv.Atoi(pa[i])
		y, errB := strconv.Atoi(pb[i])
		if errA != nil || errB != nil {
			return a != b
		}
		if x != y {
			return x > y
		}
	}
	return false
}

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

func launchConnectorHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST to mint a connector token")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, agentDirUnresolvedMessage)
		return
	}
	path := pairing.StorePath(dir)
	me, _ := authorizedDevice(path, r.Header.Get("Authorization"))
	name := "connector"
	if me != "" {
		name = me + " · connector"
	}
	connectorToken, err := pairing.PairLocal(path, name)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not mint a connector token")
		return
	}
	writeLaunchJSON(w, map[string]any{"name": name, "token": connectorToken, "url": strings.TrimSuffix(launcherURL(), "/app") + "/mcp"})
}

func launchDevicesHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, agentDirUnresolvedMessage)
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
				"name": d.Name, "pairedAt": d.CreatedAt.UnixMilli(), "current": d.Name == me, "encrypted": d.Encrypted(), "role": d.Role,
			})
		}
		writeLaunchJSON(w, map[string]any{"devices": list})
	case http.MethodDelete:
		name := launchNameArg(r)
		if name == "" {
			writeLaunchError(w, http.StatusBadRequest, "a device name is required")
			return
		}
		if name == me {
			writeLaunchError(w, http.StatusBadRequest, "this device cannot revoke itself - do it from the laptop with `corgi mcp devices revoke`")
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
		_ = push.Load(dir).Remove(name)
		writeLaunchJSON(w, map[string]any{"revoked": name})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET to list, DELETE to revoke")
	}
}

func launchDoctorHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, getOnlyMessage)
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
		if withProfile, perr := config.ApplyProfile(resolved, user, p); perr == nil {
			resolved = withProfile
		}
	}
	return ws.AbsPath, resolved.ConfigDir, true
}

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
		return nil
	}
	return []string{"https://claude.ai/code/" + bp.SessionID}
}

func mungeClaudeProjectDir(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-", "\\", "-", ":", "-").Replace(dir)
}

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
		startTokens.seen = map[int]startToken{}
	}
	startTokens.seen[pid] = startToken{value: value, read: time.Now()}
}

func linuxStartTicks(pid int) (string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	closing := strings.LastIndex(string(data), ")")
	if closing < 0 {
		return "", false
	}
	fields := strings.Fields(string(data)[closing+1:])
	const startTimeOffset = 19
	if len(fields) <= startTimeOffset {
		return "", false
	}
	return fields[startTimeOffset], true
}

func darwinStartDate(pid int) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
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

func listClaudeSessions(absPath, configDir string) []claudeSession {
	if sessions, ok := localClaudeSessions(absPath, configDir); ok {
		return sessions
	}
	return claudeAgentsSessions(absPath, configDir)
}

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

func launchNameArg(r *http.Request) string {
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			Name string `json:"name"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if json.Unmarshal(raw, &body) == nil && strings.TrimSpace(body.Name) != "" {
			return strings.TrimSpace(body.Name)
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("name"))
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

const launcherPageHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" media="(prefers-color-scheme:light)" content="#fbfbfc">
<meta name="theme-color" media="(prefers-color-scheme:dark)" content="#15161a">
<title>corgi</title>
<style>
  :root{color-scheme:light dark;
      --bg:#fbfbfc;--raise:#ffffff;--hover:#eeeff2;--line:#e1e2e7;
      --text:#1a1c21;--dim:#5a5e68;--accent:#4450c8;--fill:#4f5bd5;--on-fill:#ffffff;
      --ok:#1a7336;--warn:#8a4b00;--bad:#b3261e;
      --s1:4px;--s2:8px;--s3:12px;--s4:16px;--s5:24px;--s6:32px;
      --page:92rem;--read:56rem;--edge:24px;--tap:0px}
  @media (prefers-color-scheme:dark){:root{
      --bg:#15161a;--raise:#1c1d22;--hover:#24262b;--line:#2c2e34;
      --text:#e6e7ea;--dim:#9b9fa9;--accent:#a5aefb;--fill:#5e6ad2;--on-fill:#ffffff;
      --ok:#62c584;--warn:#e0a652;--bad:#f28b82}}
  @media (max-width:760px){:root{--edge:16px}}
  @media (pointer:coarse){:root{--tap:10px}}
  *{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
  [hidden]{display:none!important}
  html{-webkit-text-size-adjust:100%;touch-action:manipulation}
  body{margin:0;background:var(--bg);color:var(--text);padding-bottom:env(safe-area-inset-bottom);
      font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,sans-serif;
      -webkit-font-smoothing:antialiased;overscroll-behavior-y:none}
  h1,h2,h3,h4,p{margin:0}
  a{color:inherit}
  code{font:.92em ui-monospace,SFMono-Regular,Menlo,monospace}
  :focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:4px}

  button{font:inherit;font-size:13px;font-weight:500;color:var(--text);background:none;cursor:pointer;
      border:1px solid var(--line);border-radius:6px;padding:calc(5px + var(--tap)/2) 12px;touch-action:manipulation}
  button:hover{background:var(--hover)}
  button:disabled{opacity:.5;cursor:default}
  button.fill{background:var(--fill);border-color:var(--fill);color:var(--on-fill)}
  button.fill:hover{filter:brightness(1.08)}
  .text,.erow>*,.sact>*,.actions>button{border:0;border-radius:4px;background:none;padding:calc(2px + var(--tap)) 0;
      font-size:13px;font-weight:500;color:var(--dim);text-decoration:none;cursor:pointer}
  .text:hover,.erow>*:hover,.sact>*:hover,.actions>button:hover{background:none;color:var(--text);text-decoration:underline;text-underline-offset:3px}
  .erow .primary,.sact .ok,.text.primary{color:var(--accent)}
  .erow,.sact,.actions{display:flex;flex-wrap:wrap;column-gap:var(--s4);row-gap:0;align-items:baseline;margin-top:var(--s1)}
  input,textarea,select{font:inherit;color:inherit;background-color:var(--raise);border:1px solid var(--line);
      border-radius:6px;padding:7px 10px;touch-action:manipulation}
  textarea{resize:vertical}
  ::placeholder{color:var(--dim);opacity:1}
  select{-webkit-appearance:none;appearance:none;cursor:pointer;text-overflow:ellipsis;padding-inline-end:26px;
      background-repeat:no-repeat;background-position:right 10px center;
      background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M1 1l4 4 4-4' fill='none' stroke='%23838791' stroke-width='1.5'/%3E%3C/svg%3E")}
  input[type=checkbox]{width:16px;height:16px;margin:0;padding:0;flex:0 0 auto;accent-color:var(--fill);cursor:pointer}
  @media (pointer:coarse){select,textarea,input:not([type=checkbox]):not([type=radio]){font-size:16px}
      input[type=checkbox]{width:22px;height:22px}}

  header{position:sticky;top:0;z-index:30;background:var(--bg);border-bottom:1px solid var(--line);
      padding:env(safe-area-inset-top) max(var(--edge),env(safe-area-inset-right)) 0 max(var(--edge),env(safe-area-inset-left))}
  .bar{max-width:var(--page);min-height:49px;margin:0 auto;display:grid;align-items:center;column-gap:var(--s5);
      grid-template-columns:auto minmax(0,1fr) auto auto;grid-template-areas:"name tabs host again"}
  h1{grid-area:name;font-size:15px;font-weight:600}
  #host{grid-area:host;color:var(--dim);font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  #host.down{color:var(--bad)}
  #host .what{text-decoration:underline;text-decoration-color:var(--line);text-underline-offset:3px;cursor:pointer}
  #refresh{grid-area:again;font-size:15px;line-height:1;padding:calc(6px + var(--tap)/2) 8px;border:0;color:var(--dim)}
  #refresh i{display:block;font-style:normal}
  #refresh.spin i{animation:spin .7s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .hostnote{max-width:var(--page);margin:0 auto;padding:0 0 var(--s3);color:var(--dim);font-size:13px}
  .tabs{grid-area:tabs;display:flex;gap:var(--s5);overflow-x:auto;scrollbar-width:none}
  .tabs::-webkit-scrollbar{display:none}
  .tabs button{flex:0 0 auto;border:0;border-radius:0;padding:14px 0 13px;color:var(--dim);font-size:14px;font-weight:500;border-bottom:2px solid transparent}
  .tabs button:hover{background:none;color:var(--text)}
  .tabs button[aria-selected=true]{color:var(--text);border-bottom-color:var(--text)}
  .tabs .n{margin-inline-start:6px;font-weight:400;color:var(--dim);font-variant-numeric:tabular-nums}
  .tabs .n:empty{display:none}
  @media (max-width:980px){.bar{grid-template-columns:auto minmax(0,1fr) auto;grid-template-areas:"name host again" "tabs tabs tabs"}
      h1{padding-top:10px}#host{padding-top:12px;text-align:end}#refresh{margin-top:8px}.tabs button{padding:10px 0 9px}}

  main{max-width:calc(var(--page) + 2*var(--edge));margin:0 auto;
      padding:var(--s5) max(var(--edge),env(safe-area-inset-right)) calc(80px + env(safe-area-inset-bottom)) max(var(--edge),env(safe-area-inset-left))}
  [data-pane]{max-width:var(--read)}
  [data-pane=kanban]{max-width:none}
  .msg{color:var(--dim)}
  .err{color:var(--bad)}
  .lead{color:var(--dim);margin-bottom:var(--s4)}
  .empty{padding:var(--s5) 0}
  .empty h2{font-size:15px;font-weight:600;margin-bottom:var(--s1)}
  .empty p{color:var(--dim);max-width:60ch}
  .empty p+p{margin-top:var(--s2)}
  .empty button{margin-top:var(--s3)}
  .barline{display:flex;flex-wrap:wrap;align-items:baseline;gap:var(--s2) var(--s4);margin-bottom:var(--s4)}
  .barline .sum{flex:1 1 auto;min-width:12rem}
  .sum{color:var(--dim)}
  .sum b{font-weight:500;color:var(--text)}
  .sum .now{color:var(--warn);font-weight:500}
  .gh{display:flex;align-items:baseline;gap:var(--s2);margin:0 0 var(--s1);font-size:13px;font-weight:600}
  #inbox>section+section{margin-top:var(--s6)}
  .gh .n{font-weight:400;color:var(--dim)}
  .row{padding:var(--s3) 0;border-top:1px solid var(--line)}
  .rtop{display:flex;flex-wrap:wrap;align-items:baseline;gap:0 var(--s2);font-size:12px;color:var(--dim)}
  .rtop input{align-self:center}
  .ref{color:var(--dim);text-decoration:none}
  a.ref:hover{color:var(--accent);text-decoration:underline;text-underline-offset:3px}
  .title{font-weight:500;overflow-wrap:anywhere}
  .facts{font-size:12px;color:var(--dim);margin-top:2px;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}
  .facts.why{font-size:13px}
  .words{margin-top:var(--s1);font-size:13px;color:var(--dim);white-space:pre-wrap;overflow-wrap:anywhere;max-width:72ch}
  .words:not(.all){display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}
  .words.all{color:var(--text)}
  .say{display:block;width:100%;max-width:40rem;margin:var(--s2) 0 var(--s1)}

  .kb{display:grid;grid-auto-flow:column;gap:var(--s5);align-items:start;overflow-x:auto;padding-bottom:var(--s3)}
  .kcol{min-width:0}
  .kcol h2{font-size:13px;font-weight:600;padding-bottom:var(--s2);border-bottom:1px solid var(--line);display:flex;gap:var(--s2)}
  .kcol h2 .n{font-weight:400;color:var(--dim);font-variant-numeric:tabular-nums}
  .kcol .none{padding:var(--s3) 0;font-size:12px;color:var(--dim)}
  .kcol .row:nth-of-type(1){border-top:0}
  .kfeat{margin-bottom:var(--s5);max-width:var(--read)}
  .kdone{margin-top:var(--s6)}
  .kgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(15rem,1fr));column-gap:var(--s5)}
  @media (max-width:959px){.kb{display:block;overflow:visible}.kcol{margin-bottom:var(--s6)}.kcol.zero{display:none}}

  .acct{display:flex;flex-wrap:wrap;gap:var(--s1) var(--s5);margin-bottom:var(--s3);font-size:13px;color:var(--dim);font-variant-numeric:tabular-nums}
  .acct b{color:var(--text);font-weight:500}
  .acct .bar5{display:inline-block;width:56px;height:4px;border-radius:2px;background:var(--line);vertical-align:middle;margin:0 var(--s2);overflow:hidden}
  .acct .bar5 i{display:block;height:100%;background:var(--dim)}
  .acct .bar5.warm i{background:var(--warn)}
  .acct .bar5.hot i,.acct .out{color:var(--bad)}
  .acct .bar5.hot i{background:var(--bad)}
  .newchat{border:1px solid var(--line);border-radius:8px;background:var(--raise);padding:var(--s3);margin-bottom:var(--s5)}
  .newchat textarea{display:block;width:100%;border:0;padding:0;background:none;min-height:44px}
  .newchat textarea:focus-visible{outline:0}
  .newchat:focus-within{border-color:var(--accent)}
  .nrow{display:flex;flex-wrap:wrap;gap:var(--s2);margin-top:var(--s2);align-items:center}
  .nrow select{flex:0 1 auto;min-width:0;max-width:14rem;font-size:13px;padding-block:calc(5px + var(--tap)/2)}
  .nrow button{margin-inline-start:auto}
  .sess{display:grid;grid-template-columns:8px minmax(0,1fr) auto;column-gap:var(--s3);padding:var(--s3) 0;border-top:1px solid var(--line);align-items:baseline}
  .sess>*:not(.sdot):not(.slabel):not(.sdetail){grid-column:2/-1}
  .sdot,.dot{width:8px;height:8px;border-radius:50%;background:var(--line);flex:0 0 auto;align-self:center}
  .sdot.needs,.dot.attention{background:var(--warn)}
  .sdot.working,.dot.starting{background:var(--accent)}
  .sdot.limited,.dot.blocked{background:var(--bad)}
  .dot.live,.sdot.on{background:var(--ok)}
  .dot.ready{background:none;border:1.5px solid var(--ok)}
  .slabel{font-weight:500;overflow-wrap:anywhere}
  .sbadge{font-weight:400;font-size:12px;color:var(--dim);margin-inline-start:var(--s2)}
  .sdetail{font-size:12px;color:var(--dim);text-align:end;overflow-wrap:anywhere}
  .ssum{font-size:12px;color:var(--dim);overflow-wrap:anywhere}

  .ws{padding:var(--s4) 0;border-top:1px solid var(--line)}
  .ws:first-child{border-top:0;padding-top:0}
  .ws .head{display:flex;align-items:flex-start;gap:var(--s4)}
  .ws .main{min-width:0;flex:1}
  .ws .name{display:flex;align-items:center;gap:var(--s2);font-weight:500}
  .ws .path{font-size:12px;color:var(--dim);margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;cursor:pointer}
  .ws .path.full{white-space:normal;overflow-wrap:anywhere}
  .ws .wnote{color:var(--bad);font-size:13px;margin-top:var(--s1)}
  .meta,.usage{font-size:12px;color:var(--dim);margin-top:2px;font-variant-numeric:tabular-nums}
  .meta span+span::before{content:"\00b7";margin:0 6px;color:var(--dim)}
  .meta .live{color:var(--ok)}
  .meta .warn{color:var(--bad)}
  a.open{display:inline-block;text-decoration:none;font-size:13px;font-weight:500;color:var(--accent);border:1px solid var(--line);border-radius:6px;padding:calc(5px + var(--tap)/2) 12px}
  a.open:hover{background:var(--hover)}
  .top{display:flex;align-items:baseline;justify-content:space-between;gap:var(--s3);margin-top:var(--s2);font-size:13px;text-decoration:none}
  .top>span:first-child,.s>span:first-child{display:flex;align-items:center;gap:var(--s2);min-width:0}
  a.top:hover .tname,a.s:hover .tname{text-decoration:underline;text-underline-offset:3px}
  .tname{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .when{flex:0 0 auto;max-width:60%;font-size:12px;color:var(--dim);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .actions{margin-top:var(--s2)}
  .actions .danger:hover{color:var(--bad)}
  .actions b{font-weight:500;color:var(--text)}
  .startbox{flex-basis:100%;display:none;flex-wrap:wrap;gap:var(--s2);margin-top:var(--s2)}
  .startbox.on{display:flex}
  .startbox select,.startbox input{flex:1 1 12rem;min-width:0}
  .sessions{margin-top:var(--s3);padding-inline-start:var(--s4)}
  .sessions .s,.evrow{display:flex;align-items:baseline;justify-content:space-between;gap:var(--s3);padding:calc(4px + var(--tap)/2) 0;font-size:13px;text-decoration:none}
  .evrow{color:var(--dim);font-size:12px}
  .evrow b{font-weight:400}
  a.s.past .tname{color:var(--dim)}
  .sessions .grp{margin:var(--s3) 0 2px;font-size:12px;font-weight:600}
  .sessions .grp i{display:none}
  .sessions .none,.sessions .hint{color:var(--dim);font-size:12px;padding:2px 0;max-width:72ch}
  .kv{display:flex;gap:var(--s3);font-size:12px;padding:2px 0}
  .kv b{color:var(--dim);font-weight:400;flex:0 0 6rem}
  .tag{font-size:12px;color:var(--dim)}
  .linkme{margin-inline-start:var(--s2);font-size:12px;padding:0 6px}
  .revealer{margin-top:var(--s4)}

  a.idea{display:block;text-decoration:none}
  .idea .itop{display:flex;align-items:baseline;gap:var(--s4)}
  .idea .title{flex:1;min-width:0}
  a.idea:hover .title{text-decoration:underline;text-underline-offset:3px}

  .sec{padding:var(--s5) 0;border-top:1px solid var(--line)}
  .sec:first-of-type{border-top:0;padding-top:0}
  .sec h2{font-size:15px;font-weight:600;margin-bottom:var(--s1)}
  .sec p{color:var(--dim);max-width:72ch;margin-bottom:var(--s2)}
  .sec p b{font-weight:500;color:var(--text)}
  pre{margin:var(--s2) 0;padding:var(--s3);border:1px solid var(--line);border-radius:6px;background:var(--raise);overflow-x:auto;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
  .toggle{display:flex;align-items:center;gap:var(--s2);padding:var(--s2) 0;cursor:pointer}
  .dev{display:flex;align-items:center;justify-content:space-between;gap:var(--s3);padding:var(--s2) 0;border-top:1px solid var(--line)}
  .dev .sub{color:var(--dim);font-size:12px}
  .chk{display:flex;gap:var(--s2);padding:var(--s2) 0;border-top:1px solid var(--line)}
  .chk b{font-weight:500}
  .chk .fix{display:block;color:var(--dim);font-size:13px}
  .chk .mark{color:var(--ok)}
  .chk.bad .mark{color:var(--bad)}
  .tip{display:block;width:100%;text-align:start;border:0;border-top:1px solid var(--line);border-radius:0;padding:var(--s3) 0;font-weight:400}
  .tip:hover{background:none}
  .tip-t{display:block;font-size:14px;font-weight:500}
  .tip-d{display:block;color:var(--dim);max-width:72ch}
  .tip-d b{font-weight:500;color:var(--text)}
  .tip-cmd{display:flex;align-items:baseline;gap:var(--s4);margin-top:var(--s1)}
  .tip-cmd code{min-width:0;overflow-x:auto;white-space:nowrap}
  .tip-copy{flex:0 0 auto;color:var(--accent);font-weight:500}
  .tip.copied .tip-copy{color:var(--ok)}
  .foot{margin-top:var(--s5);color:var(--dim)}

  .is-run{color:var(--accent)}
  .is-wait{color:var(--warn)}
  .is-bad{color:var(--bad)}

  .scrim{position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:60;display:flex;align-items:flex-end;justify-content:center}
  .sheet{width:100%;max-height:82vh;overflow:auto;background:var(--raise);border-top:1px solid var(--line);
      border-radius:12px 12px 0 0;padding:var(--s4) var(--s4) calc(var(--s4) + env(safe-area-inset-bottom))}
  .sheet.wide{--sheet:56rem}
  .grab{width:36px;height:4px;border-radius:2px;background:var(--line);margin:0 auto var(--s3)}
  @media (min-width:761px){.scrim{align-items:center;padding:var(--s5)}
      .sheet{width:min(var(--sheet,32rem),100%);border:1px solid var(--line);border-radius:10px;padding:var(--s5)}.grab{display:none}}
  .sheet h2{font-size:15px;font-weight:600;margin-bottom:var(--s3);overflow-wrap:anywhere}
  .sheet h3{font-size:12px;font-weight:600;margin:var(--s4) 0 var(--s1)}
  .sheet .tin{display:block;width:100%;margin-bottom:var(--s2)}
  .sheet .end{display:flex;justify-content:flex-end;gap:var(--s2);margin-top:var(--s3)}
  .opt{display:block;width:100%;text-align:start;border:0;border-top:1px solid var(--line);border-radius:0;padding:calc(9px + var(--tap)/2) var(--s2);font-size:14px;font-weight:400}
  .opt.here{color:var(--dim)}
  .hrow{display:grid;grid-template-columns:4.6rem minmax(0,1fr) auto;gap:var(--s3);padding:var(--s1) 0;font-size:13px;font-variant-numeric:tabular-nums}
  .hrow .t,.hrow .facts{color:var(--dim);white-space:nowrap}
  .runlog{margin:0;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere;border:0;padding:0;background:none}
  .batch{display:flex;flex-wrap:wrap;align-items:center;gap:var(--s2);padding:var(--s2) 0}
  .batch .bcount{color:var(--dim);margin-inline-end:var(--s2);font-variant-numeric:tabular-nums}
  .toast{position:fixed;left:50%;bottom:calc(20px + env(safe-area-inset-bottom));transform:translateX(-50%);z-index:70;max-width:calc(100% - 32px);
      background:var(--text);color:var(--bg);font-size:13px;font-weight:500;padding:10px 14px;border-radius:8px;opacity:0;transition:opacity .16s}
  .toast.on{opacity:1}
  .toast.bad::before{content:"\2715";margin-inline-end:8px}
  @media (prefers-reduced-motion:reduce){.toast{transition:none}#refresh.spin i{animation:none}}
</style>
<header>
  <div class="bar">
    <h1>corgi</h1>
    <div class="tabs" id="tabs" role="tablist">
      <button role="tab" data-tab="ideas" aria-selected="true">Ideas<span class="n" id="n-ideas"></span></button>
      <button role="tab" data-tab="sessions" aria-selected="false">Agents<span class="n" id="n-sessions"></span></button>
      <button role="tab" data-tab="kanban" aria-selected="false">Board<span class="n" id="n-kanban"></span></button>
      <button role="tab" data-tab="stacks" aria-selected="false">Repos<span class="n" id="n-stacks"></span></button>
      <button role="tab" data-tab="inbox" aria-selected="false">Inbox<span class="n" id="n-inbox"></span></button>
      <button role="tab" data-tab="laptop" aria-selected="false">Laptop</button>
      <button role="tab" data-tab="settings" aria-selected="false">Settings</button>
    </div>
    <span id="host">your machine</span>
    <button id="refresh" aria-label="Refresh" title="Refresh"><i>&#x21bb;</i></button>
  </div>
  <p id="hostnote" class="hostnote" hidden></p>
</header>
<main>
  <div data-pane="inbox" hidden>
    <section id="inbox"><p class="msg">Loading…</p></section>
  </div>
  <div data-pane="kanban" hidden>
    <section id="kanban"><p class="msg">Loading…</p></section>
  </div>
  <div data-pane="sessions" hidden>
    <section id="newchat" hidden></section>
    <section id="board"><p class="msg">Loading…</p></section>
  </div>
  <div data-pane="stacks" hidden>
    <div id="list" class="msg">Loading…</div>
  </div>
  <div data-pane="ideas">
    <p class="lead">A page for one thing to build. Share it, the team comments, the agent answers. Make one with <code>corgi idea new "…"</code> or <code>corgi idea share spec.md</code>.</p>
    <div id="ideas" class="msg">Loading…</div>
  </div>
  <div data-pane="laptop" hidden>
  <div id="tips" hidden>
    <p class="lead" id="tipmsg">Commands to run on the laptop. Choose one to copy it.</p>
    <button class="tip" data-copy="/corgi:remote">
      <span class="tip-t">Set it all up, guided</span>
      <span class="tip-d">The corgi plugin registers the repo, the tunnel and the start at login, from inside Claude Code.</span>
      <span class="tip-cmd"><code>/corgi:remote</code><span class="tip-copy">Copy</span></span>
    </button>
    <button class="tip" data-copy="corgi agent init">
      <span class="tip-t">Add another repo</span>
      <span class="tip-d">Run it inside any git repo and it joins this list.</span>
      <span class="tip-cmd"><code>cd ~/dev/api &amp;&amp; corgi agent init</code><span class="tip-copy">Copy</span></span>
    </button>
    <button class="tip" data-copy="corgi agent tunnel setup corgi.yourdomain.com">
      <span class="tip-t">Keep this link working</span>
      <span class="tip-d">The free tunnel changes address on every restart, which un-pairs your phone. Point it at a host you own once.</span>
      <span class="tip-cmd"><code>corgi agent tunnel setup corgi.yourdomain.com</code><span class="tip-copy">Copy</span></span>
    </button>
    <button class="tip" data-copy="corgi agent init --config-dir ~/.claude-work">
      <span class="tip-t">Use a second Claude account</span>
      <span class="tip-d">Give a work repo its own account, then set its links to <b>Open in chrome</b>.</span>
      <span class="tip-cmd"><code>corgi agent init --config-dir ~/.claude-work</code><span class="tip-copy">Copy</span></span>
    </button>
    <button class="tip" data-copy="corgi agent install">
      <span class="tip-t">Survive a reboot</span>
      <span class="tip-d">Starts corgi at login, so this page works when you are away from the desk.</span>
      <span class="tip-cmd"><code>corgi agent install</code><span class="tip-copy">Copy</span></span>
    </button>
    <button class="tip" data-copy="corgi agent hooks enable --all">
      <span class="tip-t">Tell me when a session needs me</span>
      <span class="tip-d">A session waiting on a permission prompt does not show here. <b>--all</b> covers every repo in this list. These reach the laptop; <b>corgi agent notify telegram --token …</b> sends them to the phone too.</span>
      <span class="tip-cmd"><code>corgi agent hooks enable --all</code><span class="tip-copy">Copy</span></span>
    </button>
  </div>
  </div>
  <div data-pane="settings" hidden>
  <div id="settings" hidden>
    <section class="sec">
    <h2>Claude app</h2>
    <p>Add corgi as a custom connector on claude.ai (Settings → Connectors → Add custom) to drive this machine from the Claude app too.</p>
    <pre id="cfg"></pre>
    <button id="copycfg">Copy connector config</button>
    <p id="copymsg" class="msg"></p>
    </section>

    <section class="sec">
    <h2>This browser</h2>
    <p><b>Open in</b> on a repo says where its session links open: <b>app</b> in the Claude app, <b>browser</b> here, <b>chrome</b> in Chrome, for a repo on another Claude account. Kept per repo, in this browser.</p>
    <p><b>Hide</b> takes a repo off this screen when you show it to someone. Nothing on the machine changes.</p>
    <label class="toggle"><input type="checkbox" id="showbridges"> Show sessions started by hand (bridge)</label>
    <p>A bridge is a session started on the laptop itself. Its claude.ai page shows only what you send from here; the whole transcript stays on the laptop.</p>
    </section>

    <section class="sec">
    <h2>Devices</h2>
    <p>Everything that scanned a pairing QR. Revoking one leaves the others working.</p>
    <div id="devices" class="msg">Loading…</div>
    </section>

    <section class="sec">
    <h2>If something will not start</h2>
    <p>The same checks as <code>corgi agent doctor</code>, run from here.</p>
    <button id="rundoctor">Run doctor</button>
    <div id="doctor"></div>
    <p>To get a push when a session needs you, set <code>notifyUrl</code> in the agent config on the laptop: Telegram, Slack, Discord or ntfy. Without it, notifications reach the laptop only.</p>
    </section>
  </div>
  <p class="foot"><a id="allsessions" target="_blank" rel="noopener">All your sessions on claude.ai</a></p>
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

  const make = (tag, cls, text) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  const press = (text, cls, fn) => { const b = make('button', cls, text); b.type = 'button'; b.onclick = fn; return b; };
  const httpsLink = (url, text, cls) => {
    const a = make('a', cls, text);
    a.href = url; a.target = '_blank'; a.rel = 'noopener noreferrer';
    return a;
  };
  const isHttps = url => /^https:\/\//.test(url || '');
  const shortRef = ref => { const t = String(ref || ''); return t.slice(t.lastIndexOf('/') + 1) || t; };
  const refLabel = (ref, url) => {
    const el = isHttps(url) ? httpsLink(url, shortRef(ref), 'ref') : make('span', 'ref', shortRef(ref));
    if (shortRef(ref) !== ref) el.title = ref;
    return el;
  };
  // Long text starts at two lines, and stays open once opened: the page
  // redraws every few seconds.
  const readOn = new Set();
  function longWords(key, text) {
    const wrap = make('div');
    const p = make('div', 'words', text);
    const more = press('', 'text', () => { readOn.has(key) ? readOn.delete(key) : readOn.add(key); paint(); });
    const paint = () => {
      const open = readOn.has(key);
      p.classList.toggle('all', open);
      more.textContent = open ? 'Show less' : 'Show all';
      more.setAttribute('aria-expanded', String(open));
      more.hidden = !open && p.scrollHeight <= p.clientHeight + 1;
    };
    wrap.append(p, more);
    paint();
    new ResizeObserver(paint).observe(p);
    return wrap;
  }
  // A redraw never takes a half-typed comment or a selection with it.
  const inUse = box => [...box.querySelectorAll('input.say')].some(i => i.value || i === document.activeElement) ||
    [...box.querySelectorAll('input[type=checkbox]')].some(i => i.checked);
  function failed(box, again) {
    if (box.dataset.drawn) return;
    box.textContent = '';
    const e = make('div', 'empty');
    e.append(make('h2', '', 'This page cannot reach corgi'), make('p', '', 'The laptop may be asleep or offline. On it, corgi agent status says more.'), press('Try again', '', again));
    box.appendChild(e);
  }

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

  // One failure, one line, over the thumb - never a red block that pushes the
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
    // land in a pane that is hidden unless you happen to be on Stacks - which
    // is how this page came to render as a blank screen.
    document.getElementById('tabs').hidden = true;
    for (const p of document.querySelectorAll('[data-pane]')) {
      p.hidden = true;
    }
    const pair = document.createElement('div');
    pair.className = 'empty';
    pair.innerHTML = '<h2>Pair this browser</h2><p>This page needs a key before it can show anything. ' +
      'On this laptop run <code>corgi agent dashboard</code>. On a phone run <code>corgi agent up</code> ' +
      'on the laptop and scan the QR it prints.</p><p>The key is kept in this browser only.</p>';
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
    list.className = 'msg';
    list.textContent = 'Loading…';
  }

  // A session's name and its state both change while you are looking at them:
  // Claude renames a session as the work takes shape, a permission prompt
  // arrives, a session exits. Refresh on a slow tick - only while the page is
  // on screen, and never while a panel is open under the thumb, since
  // re-rendering would collapse it.
  function initRefresh() {
    const btn = document.getElementById('refresh');
    // The button is a real reload: the daemon rescans and polls every tracker
    // now, and this page - with the phone and the menu bar - re-reads once
    // it has had a moment to publish.
    btn.onclick = () => {
      btn.classList.add('spin');
      fetch('/launch/refresh', { method: 'POST', headers: auth }).catch(() => {});
      const reread = () => Promise.all([load(), loadInfo(), loadBoard()]);
      reread().then(() => setTimeout(reread, 1500)).finally(() => setTimeout(() => btn.classList.remove('spin'), 1900));
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
    loadIdeas();
  }

  // The pages: one row each, the count on the tab is what waits on a person.
  async function loadIdeas() {
    const box = document.getElementById('ideas');
    if (!box) return;
    try {
      const r = await fetch('/launch/ideas', { headers: auth });
      if (!r.ok) throw new Error('HTTP ' + r.status);
      const j = await r.json();
      const ideas = j.ideas || [];
      tabCount('ideas', ideas.reduce((n, m) => n + (m.open || 0), 0));
      box.className = ''; box.innerHTML = '';
      if (!ideas.length) {
        const none = make('div', 'empty');
        none.append(make('h2', '', 'No pages yet'), make('p', '', 'On the laptop: corgi idea new "a scheduling app for a coffee shop". The agent writes the first version.'));
        box.appendChild(none);
        return;
      }
      for (const m of ideas) {
        const a = make('a', 'row idea'); a.href = '/idea/' + encodeURIComponent(m.slug);
        const top = make('div', 'itop');
        top.appendChild(make('span', 'title', m.name || m.title));
        const sh = press('Share', 'text', ev => { ev.preventDefault(); ev.stopPropagation(); shareIdea(m, sh); });
        top.appendChild(sh); a.appendChild(top);
        const sub = m.summary || (m.title !== (m.name || m.title) ? m.title : '');
        if (sub) a.appendChild(make('div', 'facts why', sub));
        const bits = [m.type || 'idea', m.status || 'draft', m.open ? m.open + ' open' : '', m.workspace || ''].filter(Boolean);
        const meta = make('div', 'facts', bits.join(' · '));
        if (m.forAgent) { meta.append(' · '); meta.appendChild(make('span', 'is-run', m.forAgent + ' with the agent')); }
        a.appendChild(meta); box.appendChild(a);
      }
    } catch (e) { box.className = 'msg err'; box.textContent = '✗ ' + e.message; }
  }
  loadIdeas();

  // The link for the team: the phone's own share sheet where there is one,
  // the clipboard where not. A closed page is opened first.
  async function shareIdea(m, btn) {
    let link = m.share, isPublic = m.public;
    if (!link) {
      try {
        const r = await fetch('/launch/idea', { method: 'POST', headers: auth, body: JSON.stringify({ do: 'share', slug: m.slug }) });
        const j = await r.json();
        if (!r.ok) throw new Error(j.error || 'HTTP ' + r.status);
        link = j.link; isPublic = j.public; m.share = link; m.public = isPublic;
        btn.textContent = 'Share';
      } catch (e) { toast('✗ ' + e.message); return; }
    }
    if (!link) { toast('no link yet - the dashboard has no address'); return; }
    const title = m.name || m.title;
    if (navigator.share && matchMedia('(pointer:coarse)').matches) {
      try { await navigator.share({ title, text: title + ' - read it and comment', url: link }); return; }
      catch (e) { if (e && e.name === 'AbortError') return; }
    }
    try { await navigator.clipboard.writeText(link); toast(isPublic ? 'Link copied - anyone with it reads and comments' : 'Copied - but this link works on the laptop only'); }
    catch { prompt('the link', link); }
  }

  function initSettings() {
    const s = document.getElementById('settings');
    const connector = JSON.stringify({ mcpServers: { corgi: {
      url: location.origin + '/mcp', headers: { Authorization: 'Bearer ' + token } } } }, null, 2);
    document.getElementById('cfg').textContent = connector;
    s.hidden = false;
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
    const msg = document.getElementById('tipmsg');
    for (const tip of box.querySelectorAll('.tip')) {
      tip.onclick = async () => {
        const text = tip.dataset.copy;
        try {
          await navigator.clipboard.writeText(text);
          tip.classList.add('copied');
          const label = tip.querySelector('.tip-copy');
          if (label) label.textContent = 'Copied';
          msg.textContent = 'Copied. Paste it in a terminal on that machine.';
          setTimeout(() => {
            tip.classList.remove('copied');
            if (label) label.textContent = 'Copy';
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
        el.innerHTML = '<span class="mark">' + (c.ok ? '✓' : '✗') + '</span><span><b>' + esc(c.name) + '</b> - ' +
          esc(c.detail || '') + (c.fix ? '<span class="fix">fix: ' + esc(c.fix) + '</span>' : '') + '</span>';
        box.appendChild(el);
      }
    } catch (e) { box.className = 'msg err'; box.textContent = '✗ ' + e.message; }
  }

  async function load() {
    try {
      const r = await fetch('/launch/workspaces', { headers: auth });
      if (r.status === 401) throw new Error('This browser is not paired any more. On the laptop run corgi agent dashboard.');
      if (!r.ok) throw new Error('This page cannot reach corgi on the laptop. On it, corgi agent status says more.');
      const j = await r.json();
      window.__workspaces = (j.workspaces || []).map(w => w.id);
      render(j.workspaces || []);
    } catch (e) {
      if (!lastWorkspaces.length) { list.className = 'msg err'; list.textContent = e.message; }
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
      bar.className = 'bar5' + (pct >= 90 ? ' hot' : pct >= 70 ? ' warm' : '');
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
    loadInbox();
    loadKanban();
    try {
      const r = await fetch('/launch/board', { headers: auth });
      if (!r.ok) { failed(box, loadBoard); return; }
      const j = await r.json();
      box.dataset.drawn = '1';
      renderNewChat(j);
      const sessions = (j.sessions || []).filter(s => s.status !== 'gone');
      tabCount('sessions', sessions.length);
      box.innerHTML = '';
      renderAccounts(j.accounts, box);
      if (!sessions.length) {
        const none = make('div', 'empty');
        none.append(make('h2', '', 'No agent is running'), make('p', '', 'A Claude session on this machine shows up here by itself. Start one above, or in a terminal.'));
        box.appendChild(none);
        return;
      }
      const hot = sessions.filter(s => s.status === 'needs_input').length;
      const working = sessions.filter(s => s.status === 'working').length;
      const rest = sessions.length - hot - working;
      const sum = make('p', 'sum');
      if (hot) { sum.appendChild(make('span', 'now', hot + (hot === 1 ? ' waits for you' : ' wait for you'))); }
      const others = [working ? working + ' working' : '', rest ? rest + ' idle' : ''].filter(Boolean).join(' · ');
      if (others) sum.append((hot ? ' · ' : '') + others);
      sum.style.marginBottom = 'var(--s3)';
      box.appendChild(sum);
      const rank = { needs_input: 0, working: 1, limited: 2, done: 3, stale: 4, unknown: 5 };
      const shown = sessions.slice().sort((a, b) =>
        (rank[a.status] ?? 9) - (rank[b.status] ?? 9));
      for (const s of shown) {
        const row = make('div', 'sess');
        const dot = make('span', 'sdot ' + (s.status === 'needs_input' ? 'needs' : esc(s.status || 'unknown')));
        const label = make('span', 'slabel', s.display || s.label || '?');
        if (s.profile && s.profile !== 'default') label.appendChild(make('span', 'sbadge', s.profile));
        let detail = s.status === 'needs_input' ? ['needs you', s.detail].filter(Boolean).join(': ') : s.detail || STATUS_WORD[s.status] || '';
        if (s.status === 'limited' && s.limit === 'overload') detail = 'API overloaded';
        if (s.drift && s.drift.length) detail = 'drifting: ' + s.drift[0];
        if (s.resumeAt) detail += (detail ? ' · ' : '') + 'continues ' + clock(s.resumeAt);
        if (s.context && s.context.percent >= 50) detail += (detail ? ' · ' : '') + 'context ' + s.context.percent + '%';
        row.append(dot, label, make('span', 'sdetail' + (s.status === 'needs_input' ? ' is-wait' : ''), detail));
        const line = sessionSummary(s);
        if (line) row.appendChild(make('div', 'ssum', line));
        const act = sessionActions(s);
        if (act) row.appendChild(act);
        box.appendChild(row);
      }
    } catch { failed(box, loadBoard); }
  }

  // The new-chat card is built once and only its choices refresh, so a
  // prompt being typed survives the board's periodic reload.
  const MODELS = [['', 'Default model'], ['opus', 'Opus'], ['sonnet', 'Sonnet'], ['haiku', 'Haiku']];
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
      const go = document.createElement('button'); go.type = 'button'; go.className = 'fill'; go.textContent = 'Start chat';
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
    fillSelect(prof, [['', 'Default account'], ...profiles.map(p => [p, p])], true);
    prof.hidden = !profiles.length;
    // Which checkout it opens in. A phone has no folder of its own, so
    // without this the chat lands wherever that editor window happened to
    // be - the wrong repo, under the wrong account.
    const ws = card.querySelector('.nws');
    let ids = lastWorkspaces.map(w => w.id).filter(Boolean);
    if (!ids.length) {
      // The board arrives before the workspace list on a cold load; the
      // sessions name their workspaces, which is enough to choose one.
      ids = [...new Set((j.sessions || []).map(s => s.workspace || s.display).filter(Boolean))];
    }
    fillSelect(ws, [['', 'The window\u2019s folder'], ...ids.map(i => [i, i])], true);
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
      // idle - typing into a session mid-turn lands in its own work.
      button('Link', '', async (e) => {
        const b = e.currentTarget;
        b.disabled = true; b.textContent = 'Linking\u2026';
        try {
          const r = await fetch('/launch/send', { method: 'POST', headers: auth,
            body: JSON.stringify({ session: s.id, text: '/remote-control' }) });
          const j = await r.json().catch(() => ({}));
          if (!r.ok) { toast(j.error || 'could not reach that session', true); b.disabled = false; b.textContent = 'Link'; return; }
          toast('asked ' + name + ' for a web link - it appears in a moment');
          setTimeout(loadBoard, 6000);
        } catch { toast('no connection', true); b.disabled = false; b.textContent = 'Link'; }
      });
    }
    return box.childElementCount ? box : null;
  }

  const EVENT_KIND = { 'issue.new': 'new issue', 'issue.comment': 'comment', 'pr.comment': 'PR comment', 'pr.review': 'PR review', 'review.requested': 'review request', 'ci.failed': 'red build', 'routine': 'routine', 'task': 'task' };
  // What a card says under its title while a session is on it or on its way.
  const pickedLine = (p) => {
    if (!p) return '';
    const m = Math.max(0, Math.round((Date.now() - Date.parse(p.at)) / 60000));
    return 'picked from the ' + (p.by === 'cli' ? 'command line' : p.by === 'bar' ? 'menu bar' : p.by || 'board') + ' ' + (m ? m + 'm ago' : 'just now') + ' \u00b7 waiting for a session to open';
  };
  const sessionLine = (s) => s ? 'session ' + s.label + ' \u00b7 ' + (STATUS_WORD[s.status] || s.status) : '';

  // The three things a person does to a pull request of theirs from a
  // row: out of draft, merged, closed. Only where corgi says it is theirs.
  function prButtons(row, ev, link) {
    if (!link || !/^https:\/\//.test(link)) return;
    const ready = document.createElement('button');
    ready.textContent = 'Ready for review';
    ready.title = 'Take the draft out of draft';
    ready.onclick = () => { ready.disabled = true; ticket(ev, { do: 'ready' }).finally(() => { ready.disabled = false; }); };
    row.appendChild(ready);
    const merge = document.createElement('button');
    merge.className = 'primary';
    merge.textContent = 'Merge';
    merge.onclick = () => {
      if (!confirm('Merge ' + (ev.ref || ev.key) + '?\n' + link)) return;
      merge.disabled = true;
      ticket(ev, { do: 'merge' }).finally(() => { merge.disabled = false; });
    };
    row.appendChild(merge);
    const shut = document.createElement('button');
    shut.textContent = 'Close PR';
    shut.onclick = () => {
      if (!confirm('Close ' + (ev.ref || ev.key) + ' without merging?\n' + link)) return;
      shut.disabled = true;
      ticket(ev, { do: 'close' }).finally(() => { shut.disabled = false; });
    };
    row.appendChild(shut);
  }

  // A task of your own: a title, a description, the workspace it is for.
  // Lands in Todo like a ticket; Work on it opens a session on it.
  function taskSheet(after) {
    const { sheet, close } = openSheet('Add a task');
    const title = make('input', 'tin'); title.placeholder = 'Title'; title.maxLength = 200;
    const body = make('textarea', 'tin'); body.rows = 6; body.placeholder = 'What to do, and how you will know it is done';
    const ws = make('select', 'tin');
    const none = make('option', '', 'Any workspace'); none.value = ''; ws.appendChild(none);
    for (const w of (window.__workspaces || [])) { const o = make('option', '', w); o.value = w; ws.appendChild(o); }
    const startRow = make('label', 'toggle');
    const start = make('input'); start.type = 'checkbox';
    startRow.append(start, 'Start now, without me');
    const add = press('Add to the board', 'fill', async () => {
      if (!title.value.trim()) { title.focus(); return; }
      add.disabled = true;
      try {
        const r = await fetch('/launch/task', { method: 'POST', headers: auth, body: JSON.stringify({ run: start.checked, title: title.value, body: body.value, workspace: ws.value }) });
        const j = await r.json().catch(() => ({}));
        if (!r.ok) { toast(j.error || 'that did not go through', true); add.disabled = false; return; }
        toast(j.done || 'on the board'); close(); if (after) after();
      } catch { toast('no connection', true); add.disabled = false; }
    });
    const end = make('div', 'end'); end.append(press('Cancel', '', close), add);
    sheet.append(title, body, ws, startRow, end);
    title.focus();
  }

  function openSheet(name, wide) {
    const scrim = make('div', 'scrim');
    const sheet = make('div', 'sheet' + (wide ? ' wide' : ''));
    sheet.setAttribute('role', 'dialog'); sheet.setAttribute('aria-modal', 'true'); sheet.setAttribute('aria-label', name);
    const before = document.activeElement;
    const keys = e => { if (e.key === 'Escape') close(); };
    const close = () => { scrim.remove(); document.removeEventListener('keydown', keys); if (before && before.focus) before.focus(); };
    document.addEventListener('keydown', keys);
    scrim.onclick = e => { if (e.target === scrim) close(); };
    sheet.append(make('div', 'grab'), make('h2', '', name));
    scrim.appendChild(sheet);
    document.body.appendChild(scrim);
    return { sheet, close };
  }

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
    const { sheet, close } = openSheet('Move ' + shortRef(ev.ref || ev.key) + ' to');
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
    const { sheet } = openSheet(shortRef(fx.ref) + (fx.running ? ' · running' : ''), true);
    const pre = make('pre', 'runlog', 'Loading…');
    sheet.appendChild(pre);
    try {
      const r = await fetch('/launch/run?key=' + encodeURIComponent(fx.key) + '&tail=200', { headers: auth });
      const j = await r.json();
      if (!r.ok) { pre.textContent = j.error || 'This run left no log.'; return; }
      const bits = [];
      if (j.outcome) bits.push(j.outcome);
      if (j.tokens) bits.push(humanTokens(j.tokens) + ' tok' + (j.costUSD ? ' · $' + j.costUSD.toFixed(2) : ''));
      if (j.spentPercent) bits.push(j.spentPercent + '% of the 5h window');
      if (j.handover) bits.push(j.handover);
      if (bits.length) sheet.insertBefore(make('p', 'facts', bits.join(' · ')), pre);
      pre.textContent = j.log || 'This run left no log.';
    } catch { pre.textContent = 'No connection.'; }
  }

  // The kanban: one card per ticket, column worked out by corgi. A card is
  // moved on the tracker, worked on, or unblocked - the column follows.
  const NOTHING = { Inbox: 'Nothing waits here', Ready: 'Nothing is ready', Running: 'Nothing is running', Blocked: 'Nothing is stuck', Review: 'No open pull request', Done: 'Nothing finished yet' };
  async function loadKanban() {
    const box = document.getElementById('kanban');
    let cards = [], columns = [], boards = {}, features = [];
    try {
      const r = await fetch('/launch/kanban', { headers: auth });
      if (!r.ok) { failed(box, loadKanban); return; }
      const j = await r.json();
      cards = j.cards || []; columns = j.columns || []; boards = j.boards || {}; features = j.features || [];
    } catch { tabCount('kanban', 0); failed(box, loadKanban); return; }
    const done = cards.filter(c => c.column === 'Done').length;
    tabCount('kanban', cards.length - done);
    if (inUse(box)) return;
    box.dataset.drawn = '1';
    box.innerHTML = '';
    const again = () => { loadKanban(); loadInbox(); };
    if (!cards.length) {
      const none = make('div', 'empty');
      none.append(make('h2', '', 'The board is empty'), make('p', '', 'Tickets assigned to you, your pull requests and your own tasks land here.'), press('Add a task', 'fill', () => taskSheet(again)));
      box.appendChild(none);
      return;
    }
    // Done is history: hidden until asked for, so the live work comes first.
    let showDone = false;
    try { showDone = localStorage.getItem('corgi.showDone') === '1'; } catch {}
    const bar = make('div', 'barline');
    bar.appendChild(press('Add a task', 'fill', () => taskSheet(again)));
    if (done) bar.appendChild(press(showDone ? 'Hide done' : 'Show done (' + done + ')', 'text', () => {
      try { localStorage.setItem('corgi.showDone', showDone ? '0' : '1'); } catch {}
      loadKanban();
    }));
    bar.appendChild(press('History', 'text', historySheet));
    box.appendChild(bar);
    if (features.length) box.appendChild(featureRows(features));
    const shown = columns.filter(col => col !== 'Done');
    const rowsOf = col => cards.filter(c => c.column === col);
    const wrap = make('div', 'kb');
    wrap.style.gridTemplateColumns = shown.map(col => rowsOf(col).length ? 'minmax(10rem,1fr)' : 'minmax(7rem,.4fr)').join(' ');
    for (const col of shown) {
      const rows = rowsOf(col);
      const kc = make('section', 'kcol' + (rows.length ? '' : ' zero'));
      const h = make('h2', '', col);
      h.appendChild(make('span', 'n', String(rows.length)));
      kc.appendChild(h);
      if (!rows.length) kc.appendChild(make('p', 'none', NOTHING[col] || 'Nothing here'));
      for (const c of rows) kc.appendChild(kanbanCard(c, col, boards[c.workspace] || {}));
      wrap.appendChild(kc);
    }
    box.appendChild(wrap);
    if (done && showDone) {
      const sec = make('section', 'kdone');
      const h = make('h2', 'gh', 'Done'); h.appendChild(make('span', 'n', String(done)));
      const list = make('div', 'kgrid');
      for (const c of rowsOf('Done')) list.appendChild(kanbanCard(c, 'Done', boards[c.workspace] || {}));
      sec.append(h, list);
      box.appendChild(sec);
    }
  }

  function featureRows(features) {
    const wrap = make('section', 'kfeat');
    const h = make('h2', 'gh', 'Features'); h.appendChild(make('span', 'n', String(features.length)));
    wrap.appendChild(h);
    for (const f of features) {
      const row = make('div', 'row');
      const top = make('div', 'rtop'); top.appendChild(make('span', 'ref', f.ref)); row.appendChild(top);
      if (f.title) row.appendChild(make('div', 'title', f.title));
      const why = make('div', 'facts why', '');
      why.append(make('span', f.state === 'blocked' ? 'is-bad' : f.state === 'running' ? 'is-run' : '', f.state), ' · ' + f.why);
      row.appendChild(why);
      const bits = [(f.waves || []).map(w => w.join(' + ')).join('  →  ')];
      if (f.cost && f.cost.tokens) bits.push(costBits(f.cost).join(' · '));
      row.appendChild(make('div', 'facts', bits.filter(Boolean).join(' · ')));
      wrap.appendChild(row);
    }
    return wrap;
  }

  function cardFacts(c) {
    const bits = [];
    if (c.session) bits.push(sessionLine(c.session));
    if (c.feature) bits.push('part of ' + c.feature.ref + (c.feature.title ? ' ' + c.feature.title : '') + (c.after && c.after.length ? ' · after ' + c.after.join(', ') : ''));
    if (c.handoff && c.handoff.next && !(c.why || '').includes(c.handoff.next)) bits.push('next: ' + c.handoff.next);
    const last = [c.touches && c.touches.length ? 'touches ' + c.touches.join(' + ') : '', c.branch || ''].filter(Boolean).join(' · ');
    if (last) bits.push(last);
    if (c.cost) bits.push(costBits(c.cost).join(' · '));
    return bits;
  }

  const saying = new Set();
  function kanbanCard(c, col, board) {
    const card = make('article', 'row');
    const ev = { key: c.key, ref: c.ref, workspace: c.workspace, kind: c.kind, url: c.url };
    const top = make('div', 'rtop');
    top.appendChild(refLabel(c.ref, c.url));
    if (c.workspace) top.appendChild(make('span', '', c.workspace));
    card.appendChild(top);
    if (c.title) card.appendChild(make('div', 'title', c.title));
    if (c.why) card.appendChild(make('div', 'facts why' + (col === 'Blocked' ? ' is-bad' : ''), c.why));
    if (c.now) card.appendChild(make('div', 'facts why is-run', c.now.doing + (c.now.model ? ' · ' + c.now.model : '') + (c.now.agent && c.now.agent !== 'claude' ? ' · ' + c.now.agent : '')));
    if (c.body) card.appendChild(longWords('card ' + c.key, c.body));
    for (const line of cardFacts(c)) card.appendChild(make('div', 'facts', line));
    const open = c.kind === 'task' && c.key && col !== 'Done';
    const row = make('div', 'erow');
    const workable = c.key && c.kind && (c.kind.startsWith('issue.') || c.kind === 'task');
    if (col === 'Blocked' && c.key) {
      const ub = press('Unblock', 'primary', () => { ub.disabled = true; ticket(ev, { do: 'unblock' }).finally(() => { ub.disabled = false; loadKanban(); }); });
      row.appendChild(ub);
    } else if (workable && (col === 'Inbox' || col === 'Ready' || (c.kind === 'task' && col === 'Running' && !c.session))) {
      const go = press(c.picked && !c.session ? 'Work on it again' : 'Work on it', 'primary', () => { go.disabled = true; workOn(ev).finally(() => { go.disabled = false; loadKanban(); }); });
      row.appendChild(go);
    }
    const cardPR = (c.fix && (c.fix.prs || [])[0]) || (c.session && c.session.pr) || '';
    if (isHttps(cardPR)) {
      if (cardPR !== c.url) row.appendChild(httpsLink(cardPR, cardPR.includes('/merge_requests/') ? 'Merge request' : 'Pull request'));
      if (c.key && col !== 'Done') prButtons(row, ev, cardPR);
    }
    const cols = c.columns || board.columns || [];
    if (cols.length && c.key) row.appendChild(press('Move…', '', () => moveSheet(Object.assign({ state: c.state }, ev), cols)));
    if (open) {
      const say = make('input', 'say'); say.placeholder = 'Comment, and the agent picks it up'; say.maxLength = 8000;
      say.hidden = !saying.has(c.key);
      say.onkeydown = async e => {
        if (e.key === 'Escape') { say.value = ''; say.hidden = true; saying.delete(c.key); return; }
        if (e.key !== 'Enter' || !say.value.trim()) return;
        say.disabled = true;
        try {
          const r = await fetch('/launch/task/comment', { method: 'POST', headers: auth, body: JSON.stringify({ key: c.key, text: say.value }) });
          const j = await r.json().catch(() => ({}));
          if (!r.ok) { toast(j.error || 'that did not go through', true); say.disabled = false; return; }
          say.value = ''; say.blur(); saying.delete(c.key);
          toast(j.done || 'sent'); loadKanban();
        } catch { toast('no connection', true); say.disabled = false; }
      };
      row.appendChild(press('Comment', '', () => { saying.add(c.key); say.hidden = false; say.focus(); }));
      const dn = press('Done', '', () => { dn.disabled = true; ticket(ev, { do: 'close' }).finally(() => { dn.disabled = false; }); });
      row.appendChild(dn);
      card.appendChild(row);
      card.appendChild(say);
      return card;
    }
    if (row.children.length) card.appendChild(row);
    return card;
  }

  function costBits(cost) {
    const bits = [humanTokens(cost.tokens) + ' tok'];
    if (cost.usd) bits.push('≈$' + cost.usd.toFixed(2));
    if (cost.runs) bits.push(cost.runs + (cost.runs === 1 ? ' run' : ' runs'));
    if (cost.sessions) bits.push(cost.sessions + (cost.sessions === 1 ? ' session' : ' sessions'));
    if (cost.models && cost.models.length) bits.push(cost.models.join(', '));
    if (cost.agents && cost.agents.some(a => a !== 'claude')) bits.push(cost.agents.join(' + '));
    return bits;
  }

  // The history: what already happened, day by day, from /launch/history.
  async function historySheet() {
    let entries = [];
    try {
      const r = await fetch('/launch/history?days=14', { headers: auth });
      if (r.ok) entries = (await r.json()).entries || [];
    } catch { toast('no connection', true); return; }
    const { sheet } = openSheet('The last 14 days', true);
    if (!entries.length) sheet.appendChild(make('p', 'sum', 'Nothing happened yet.'));
    let day = '';
    for (const e of entries) {
      const d = new Date(e.at);
      const label = d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
      if (label !== day) { day = label; sheet.appendChild(make('h3', '', label)); }
      const row = make('div', 'hrow');
      const what = make('span');
      what.append(refLabel(e.ref, e.url), ' ' + e.kind + (e.why && e.why !== e.kind ? ': ' + e.why : ''));
      const bits = [e.cost && e.cost.tokens ? costBits(e.cost).join(' · ') : '', e.took || ''].filter(Boolean);
      row.append(make('span', 't', d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })), what, make('span', 'facts', bits.join(' · ')));
      sheet.appendChild(row);
    }
  }

  let wholeInbox = false;
  async function loadInbox() {
    const box = document.getElementById('inbox');
    let events = [], fixes = [], boards = {};
    try {
      const r = await fetch('/launch/events', { headers: auth });
      if (!r.ok) { failed(box, loadInbox); return; }
      const j = await r.json();
      events = j.events || [];
      fixes = j.fixes || [];
      boards = j.boards || {};
    } catch { tabCount('inbox', 0); failed(box, loadInbox); return; }
    tabCount('inbox', events.length);
    if (inUse(box)) return;
    box.dataset.drawn = '1';
    box.innerHTML = '';
    const again = () => { loadInbox(); loadKanban(); };
    if (!events.length && !fixes.length) {
      const none = make('div', 'empty');
      none.append(make('h2', '', 'Nothing waits for you'), make('p', '', 'New tickets, review comments and your own tasks land here.'), press('Add a task', 'fill', () => taskSheet(again)));
      box.appendChild(none);
      return;
    }
    const running = fixes.filter(f => f.running).length;
    const toReview = fixes.filter(f => !f.running && (f.prs || []).length).length;
    const bits = [];
    if (events.length) bits.push(events.length + (events.length === 1 ? ' thing waits for you' : ' things wait for you'));
    if (toReview) bits.push(toReview + (toReview === 1 ? ' pull request corgi opened' : ' pull requests corgi opened'));
    if (running) bits.push(running + ' still running');
    const bar = make('div', 'barline');
    bar.append(make('p', 'sum', bits.join(' \u00b7 ')), press('Add a task', 'fill', () => taskSheet(again)));
    box.appendChild(bar);

    const groups = new Map();
    const shown = wholeInbox ? events : events.slice(0, 12);
    for (const ev of shown) {
      const key = ev.workspace || '';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(ev);
    }
    for (const [workspace, rows] of groups) box.appendChild(inboxGroup(workspace, rows, boards[workspace] || {}));
    if (events.length > shown.length) box.appendChild(press('Show ' + (events.length - shown.length) + ' more', 'text', () => { wholeInbox = true; loadInbox(); }));

    // Grouped by day: "corgi opened this while you slept" and "corgi opened
    // this last week" are not the same news.
    const dayOf = (iso) => {
      const t = Date.parse(iso || '');
      if (Number.isNaN(t)) return 'earlier';
      const now = new Date();
      const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
      if (t >= midnight) return 'today';
      if (t >= midnight - 86400000) return 'yesterday';
      return 'earlier';
    };
    for (const bucket of ['today', 'yesterday', 'earlier']) {
      const group = fixes.filter((f) => dayOf(f.startedAt) === bucket);
      if (group.length) box.appendChild(fixGroup(bucket, group));
    }
  }

  // One heading per workspace: a batch is shipped against one checkout.
  function inboxGroup(workspace, rows, board) {
    const wrap = make('section');
    const picked = new Set();
    const head = make('h2', 'gh', workspace || 'Elsewhere');
    head.appendChild(make('span', 'n', String(rows.length)));
    const bar = make('div', 'batch'); bar.hidden = true;
    const count = make('span', 'bcount');
    // Only new issues can be handed to one session together - a review
    // comment is about its own thread - but anything can be moved or
    // dismissed in a batch.
    const workable = () => [...picked].filter((k) => {
      const ev = rows.find((r) => r.key === k);
      return ev && ev.actionable && ev.kind === 'issue.new';
    });
    const batch = press('', '', () => {
      batch.disabled = true;
      const keys = workable();
      workOn({ keys, ref: keys.length + ' issues' }).finally(() => { batch.disabled = false; });
    });
    const clear = () => { picked.clear(); wrap.querySelectorAll('input[type=checkbox]').forEach(t => { t.checked = false; }); };
    const moveMany = press('Move…', '', () => {
      const keys = [...picked];
      moveSheet({ ref: keys.length + (keys.length === 1 ? ' ticket' : ' tickets'), keys },
        board.columns, async (status) => {
          let done = 0;
          for (const key of keys) {
            if (await ticket({ key }, { do: 'move', status }, true)) done++;
          }
          clear();
          toast(done + ' of ' + keys.length + ' moved to ' + status);
          loadInbox();
        });
    });
    const ignoreMany = press('Ignore', '', async () => {
      const keys = [...picked];
      if (!confirm('Ignore ' + keys.length + (keys.length === 1 ? ' row?' : ' rows?') +
        '\nThey leave the inbox and the unattended mode skips them. Nothing is written to the tracker.')) return;
      ignoreMany.disabled = true;
      for (const key of keys) {
        await ticket({ key }, { do: 'ignore' }, true);
      }
      clear();
      toast(keys.length + (keys.length === 1 ? ' row ignored' : ' rows ignored'));
      ignoreMany.disabled = false;
      loadInbox();
    });
    bar.append(count, batch, moveMany, ignoreMany);
    const refresh = () => {
      bar.hidden = picked.size === 0;
      count.textContent = picked.size + ' selected';
      const canShip = workable().length;
      batch.hidden = canShip < 2;
      batch.textContent = 'Ship ' + canShip + ' together';
      moveMany.hidden = !(board.columns || []).length;
    };
    wrap.append(head, bar);
    for (const ev of rows) wrap.appendChild(inboxRow(ev, board, picked, refresh));
    return wrap;
  }

  function inboxRow(ev, board, picked, refresh) {
    const card = make('article', 'row');
    const top = make('div', 'rtop');
    const tick = make('input'); tick.type = 'checkbox'; tick.setAttribute('aria-label', 'Select ' + (ev.ref || ev.key));
    tick.onchange = () => { tick.checked ? picked.add(ev.key) : picked.delete(ev.key); refresh(); };
    top.append(tick, refLabel(ev.ref || ev.key, ev.url));
    // The kind, and the column it sits in: without that a move you just made
    // looks like it did nothing.
    const kind = EVENT_KIND[ev.kind] || ev.kind;
    const what = [kind !== 'task' ? kind : '', ev.state || ''].filter(Boolean).join(' \u00b7 ');
    if (what) top.appendChild(make('span', '', what));
    card.appendChild(top);
    const said = ev.kind === 'routine' ? String(ev.title || '').replace(shortRef(ev.ref) + ' - ', '') : ev.title;
    if (said) card.appendChild(make('div', 'title', said));
    if (ev.kind === 'task' && ev.body) card.appendChild(longWords('inbox ' + ev.key, ev.body));
    if (ev.session || ev.picked) card.appendChild(make('div', 'facts why is-run', ev.session ? sessionLine(ev.session) : pickedLine(ev.picked)));
    if (ev.blocked) card.appendChild(make('div', 'facts why is-bad', 'Blocked' + (ev.blockedBy === 'breaker' ? ' after repeated failures' : ev.blockedBy === 'run' ? ' by the run' : '') + ': ' + ev.blocked));

    const row = make('div', 'erow');
    if (ev.blocked) {
      const ub = press('Unblock', 'primary', () => {
        ub.disabled = true;
        ticket(ev, { do: 'unblock' }).finally(() => { ub.disabled = false; });
      });
      row.appendChild(ub);
    }
    if (ev.actionable && !ev.blocked && !ev.session) {
      const go = press(ev.picked ? 'Work on it again' : 'Work on it', 'primary', () => {
        go.disabled = true;
        workOn(ev).finally(() => { go.disabled = false; setTimeout(loadInbox, 1500); });
      });
      row.appendChild(go);
    }
    if (ev.kind === 'routine') row.appendChild(press('Read the report', 'primary', () => runSheet({ key: ev.key.replace(/:report$/, ''), ref: ev.ref })));
    if (ev.pr) {
      if (isHttps(ev.pr) && ev.pr !== ev.url) row.appendChild(httpsLink(ev.pr, ev.pr.includes('/merge_requests/') ? 'Merge request' : 'Pull request'));
      prButtons(row, ev, ev.pr);
    }
    const cols = ev.columns || board.columns || [];
    if (cols.length && ev.kind !== 'routine') row.appendChild(press('Move…', '', () => moveSheet(ev, cols)));
    if (ev.kind === 'task') {
      const dn = press('Done', '', () => { dn.disabled = true; ticket(ev, { do: 'close' }).finally(() => { dn.disabled = false; }); });
      row.appendChild(dn);
    }
    const ig = press('Ignore', '', () => {
      ig.disabled = true;
      ticket(ev, { do: 'ignore' }).finally(() => { ig.disabled = false; });
    });
    row.appendChild(ig);
    card.appendChild(row);
    return card;
  }

  // What the unattended mode did while nobody watched, and what it opened.
  function fixGroup(bucket, fixes) {
    const wrap = make('section');
    const opened = fixes.filter((f) => (f.prs || []).length).length;
    const head = make('h2', 'gh', bucket === 'today' ? 'corgi did today' : bucket === 'yesterday' ? 'corgi did yesterday' : 'corgi did earlier');
    if (opened) head.appendChild(make('span', 'n', opened + (opened === 1 ? ' waits for your review' : ' wait for your review')));
    wrap.appendChild(head);
    for (const fx of fixes) {
      const card = make('article', 'row');
      const pr = (fx.prs || []).find(isHttps);
      const top = make('div', 'rtop'); top.appendChild(refLabel(fx.ref, pr)); card.appendChild(top);
      const line = fx.running ? 'running' : fx.error ? 'failed: ' + fx.error : pr ? '' : 'done, nothing opened';
      if (line) card.appendChild(make('div', 'facts why' + (fx.running ? ' is-run' : fx.error ? ' is-bad' : ''), line));
      const row = make('div', 'erow');
      row.appendChild(press('Log', '', () => runSheet(fx)));
      // Finishing corgi's own work without leaving the row, and only on what
      // corgi opened.
      if (pr) prButtons(row, { key: fx.key, ref: fx.ref }, pr);
      card.appendChild(row);
      wrap.appendChild(card);
    }
    return wrap;
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
      body.from = 'page';
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
      if (j.latest) bits.push('v' + j.latest + ' available - corgi upd');
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
      el.classList.toggle('down', !j.daemon);
    } catch {}
  }

  function toggleDaemonNote(up) {
    const note = document.getElementById('hostnote');
    if (!note.hidden) { note.hidden = true; return; }
    note.textContent = up
      ? 'The corgi daemon is the process on that machine that starts your sessions and keeps them running - after a crash, after a reboot, and while the laptop would otherwise sleep. This page talks to it.'
      : 'The corgi daemon is not running on that machine, so nothing here can start a session. On the laptop run: corgi agent up';
    note.hidden = false;
  }

  function render(workspaces) {
    lastWorkspaces = workspaces;
    tabCount('stacks', workspaces.length);
    if (!workspaces.length) {
      list.className = 'empty';
      list.innerHTML = '<h2>No repos yet</h2><p>On the laptop run <code>corgi agent scan ~/dev</code> ' +
        'to add the ones it finds, then refresh.</p>';
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
      sbtn.textContent = sessionsChipLabel(ws);
      sbtn.onclick = () => toggleSessions(ws, sbtn, sessionsBox);
      actions.appendChild(sbtn);
      actions.appendChild(modeSwitch(ws.id));
      const top = topSessionRow(ws);
      if (top) row.appendChild(top);
      if (!ws.running && startBox) {
        const opts = document.createElement('button');
        opts.textContent = 'Options';
        opts.onclick = () => { startBox.classList.toggle('on'); };
        actions.appendChild(opts);
      }
      if (ws.running) actions.appendChild(stopControl(ws.id));
      const hideBtn = document.createElement('button');
      hideBtn.title = 'Hide this workspace on this browser';
      hideBtn.textContent = hide.has(ws.id) ? 'Show again' : 'Hide';
      hideBtn.onclick = () => toggleHidden(ws.id);
      actions.appendChild(hideBtn);
      if (startBox) actions.appendChild(startBox);
      row.appendChild(actions);
      row.appendChild(sessionsBox);
      list.appendChild(row);
    }
    if (hiddenCount || revealHidden) {
      const b = document.createElement('button');
      b.className = 'text revealer';
      b.textContent = revealHidden ? 'Hide them again' : 'Show ' + hiddenCount + ' hidden';
      b.onclick = () => { revealHidden = !revealHidden; render(lastWorkspaces); };
      list.appendChild(b);
    }
  }

  function introLine(workspaces) {
    if (workspaces.some(w => (w.live || 0) > 0)) return null;
    const el = document.createElement('div');
    el.className = 'lead';
    el.textContent = 'Start a Claude Code session in a repo. It runs on that machine, with its files and databases; you drive it from here.';
    return el;
  }

  function sessionsChipLabel(ws) {
    const more = (ws.live || 0) - (ws.topSession ? 1 : 0);
    return more > 0 ? 'Sessions (' + (more + 1) + ')' : 'Sessions';
  }

  // Inside one repo's card the leading workspace name is noise - the card
  // already says which repo this is, so the branch and the time get the width.
  function shortSessionName(name, id) {
    const full = String(name || '');
    const prefix = id + ' \u00b7 ';
    return full.indexOf(prefix) === 0 ? full.slice(prefix.length) : full;
  }

  // Claude Code owns a session's name after it starts - /rename, a hook, or its
  // own naming all rewrite the record corgi reads - so this row shows what the
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
      el.innerHTML = '<span><i class="sdot on"></i><span class="tname">' +
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
    el.innerHTML = '<span><i class="sdot on"></i><span class="tname">' +
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
    el.textContent = fmtTokens(sum(ws.usage.today)) + ' tokens today \u00b7 ' + fmtTokens(week) + ' this week';
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

  // The branch a session here would start on, with a * for uncommitted work -
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
    if (box.style.display !== 'none') { box.style.display = 'none'; btn.textContent = sessionsChipLabel(ws); return; }
    box.style.display = ''; btn.textContent = 'Hide sessions';
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
      const tag = o.bridge ? '<span class="tag" title="Hand-started on the laptop - its web page may look empty">bridge</span>' : '';
      const text = o.label ? esc(o.label) : esc(url.split('/').pop().slice(0, 14)) + '\u2026';
      const dot = o.past ? '' : '<i class="sdot on"></i>';
      el.innerHTML = '<span>' + dot + '<span class="tname">' + text + '</span>' + tag + '</span>' +
        '<span class="when">' + esc(o.when || '') + (o.past ? 'reopen' : 'open \u2197') + '</span>';
      box.appendChild(el);
    };
    const renderLocal = (label, when, sess) => {
      const el = document.createElement('div');
      el.className = 's';
      el.innerHTML = '<span><i class="sdot on"></i><span class="tname">' + esc(label) +
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
            toast('asked ' + esc(label) + ' for a web link - it appears in a moment');
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
      if (liveCount) group('Live now');
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
      if (localOnly) note('Local only: it runs on the laptop and has no web link yet. Link asks the session for one; ask a busy session by hand with /remote-control.');
      if (bridgeRows) note('Bridge: started by hand on the laptop; its page shows only what you send from it.');
      if (bridgeHidden) note(bridgeHidden + ' bridge session' + (bridgeHidden > 1 ? 's' : '') + ' hidden. Show them in Settings.');

      if (older.length) {
        group('Earlier, not running');
        for (const row of older) renderLink(row.url, { past: true, when: row.when });
      }
      if (!liveCount && !older.length && !evs.length) {
        const none = document.createElement('div');
        none.className = 'none'; none.textContent = 'No sessions yet for this workspace.';
        box.appendChild(none);
        return;
      }
      if (evs.length) group('Activity');
      for (const ev of evs) {
        const el = document.createElement('div');
        el.className = 'evrow';
        const what = ev.kind + (ev.cause ? ' \u00b7 ' + ev.cause : '') + (ev.reason ? ' - ' + ev.reason : '');
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
    if (ws.note && ws.running) facts.push(['last exit', ws.note]);
    if (ws.wakeLock) facts.push(['wake lock', 'the machine is held awake while this runs']);
    if (ws.deviceOnly) facts.push(['device', 'online with no session - Start opens one here, or create one from the Claude app’s device list']);
    if (ws.remark) facts.push(['note', ws.remark]);
    if (ws.pid) facts.push(['pid', String(ws.pid)]);
    if (ws.disabled) facts.push(['disabled', 'the daemon stopped retrying this one - fix the cause, then Start']);
    return facts;
  }

  function renderWorkspaceFacts(ws, box, group) {
    const facts = workspaceFacts(ws);
    if (!facts.length) return;
    group('This repo');
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
  // Chrome's https handler - it forces Chrome even when this page runs in
  // Safari or the Claude app's webview.
  const chromeUrl = u => u.replace(/^https:\/\//, 'googlechromes://');

  // app mode uses a real anchor tap so iOS deep-links into the Claude app;
  // browser mode opens via JS, which keeps the session in this browser; chrome
  // mode forces Chrome via its URL scheme (right for a workspace signed into a
  // different Claude account than the app - e.g. work vs personal).
  // Where a session link opens is a per-workspace choice, so every session
  // link has to honour it - not only the one on the Stacks card, which is
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
    b.title = 'Where session links open. Press to change';
    b.innerHTML = 'Open in <b>' + esc(cur) + '</b>';
    b.onclick = () => { setOpenMode(id, order[(order.indexOf(cur) + 1) % order.length]); render(lastWorkspaces); };
    return b;
  }

  function stopControl(id) {
    const b = document.createElement('button');
    b.className = 'danger'; b.textContent = 'Stop';
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
      // Got the link, or the daemon reported why it won't start - either way, stop
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

func launchAnswerHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, answer} to answer a prompt")
		return
	}
	var req struct {
		Session   string `json:"session"`
		Answer    string `json:"answer"`
		Confirmed bool   `json:"confirmed"`
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
	if why := session.Host.Answerable(); why != "" {
		writeLaunchError(w, http.StatusConflict, "cannot answer "+firstNonEmptyString(session.Display, session.Label)+" from here: "+why)
		return
	}
	// A risky command is allowed from the phone only once its sheet showed
	// the whole command and the person confirmed it; never "always".
	if session.Pending.Risky() && (answer == "always" || (answer == "allow" && !req.Confirmed)) {
		writeLaunchError(w, http.StatusForbidden, "that command is one to read first: open the session and use Allow anyway")
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionAnswer, SessionID: session.ID, Answer: answer, Confirmed: req.Confirmed && answer == "allow", Source: "phone"})
}

func launchInterruptHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session} to interrupt a working session")
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if session.Status != sessions.StatusWorking {
		writeLaunchError(w, http.StatusConflict, "that session is not working on anything to interrupt")
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionInterrupt, SessionID: session.ID, Source: "phone"})
}

// launchCarryHandler is corgi agent carry from the phone: another account
// (profile), a fresh start (fresh), or another agent (to: claude, codex).
func launchCarryHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, to | profile | fresh} to carry a session")
		return
	}
	var req struct {
		Session string `json:"session"`
		To      string `json:"to"`
		Profile string `json:"profile"`
		Fresh   bool   `json:"fresh"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	to := strings.ToLower(strings.TrimSpace(req.To))
	if to != "" && !harness.Known(to) {
		writeLaunchError(w, http.StatusBadRequest, "to is one of "+strings.Join(harness.Names(), ", "))
		return
	}
	profile := strings.TrimSpace(req.Profile)
	if profile != "" && profile != "default" && (!profileNamePattern.MatchString(profile) || !containsString(launchProfileNames(), profile)) {
		writeLaunchError(w, http.StatusBadRequest, noSuchProfileMessage)
		return
	}
	if to == "" && !req.Fresh && profile == "" {
		writeLaunchError(w, http.StatusBadRequest, "say where: to (an agent), profile (an account) or fresh")
		return
	}
	if profile == "" {
		profile = firstNonEmpty(session.Profile, "default")
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	packetPath, err := carrySessionTo(dir, session, profile, req.Fresh, to, "phone")
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"ok": true, "done": "carrying " + session.Display, "handoff": packetPath, "to": to, "profile": profile})
}

func launchRefreshHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST to refresh everything now")
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionRefresh, Source: "phone"})
}

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
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
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
		ID      string `json:"id"`
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
	if !sendOnce.first(req.ID, time.Now()) {
		writeLaunchJSON(w, map[string]any{"done": "already typed", "duplicate": true})
		return
	}
	if session.Status == sessions.StatusGone {
		dir, _ := agentDir()
		if !policyFor(dir, session.Cwd).Headless {
			writeLaunchError(w, http.StatusConflict, "that session is closed - corgi agent watch enable --headless lets a message run it a turn anyway")
			return
		}
		launchBoardCommand(w, command.Command{Action: command.ActionContinue, SessionID: session.ID, Text: text, Source: "phone"})
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionSend, SessionID: session.ID, Text: text, Enter: true, Source: "phone"})
}

const sendOnceFor = 10 * time.Minute

type onceSet struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

var sendOnce = &onceSet{seen: map[string]time.Time{}}

func (o *onceSet) first(id string, now time.Time) bool {
	if id == "" {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for k, at := range o.seen {
		if now.Sub(at) > sendOnceFor {
			delete(o.seen, k)
		}
	}
	if at, ok := o.seen[id]; ok && now.Sub(at) <= sendOnceFor {
		return false
	}
	o.seen[id] = now
	return true
}

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
	for _, s := range rep.Ended {
		if s.ID == ref {
			return s, 0, ""
		}
	}
	return sessions.Session{}, http.StatusNotFound, "no such session on the board"
}

func launchBoardCommand(w http.ResponseWriter, c command.Command) {
	launchBoardCommands(w, []command.Command{c})
}

func launchBoardCommands(w http.ResponseWriter, cmds []command.Command) {
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
	for _, c := range cmds {
		if _, err := command.Write(dir, c); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	daemon.Nudge(info)
	out := map[string]any{"ok": true, "action": cmds[0].Action}
	if len(cmds) > 1 {
		out["count"] = len(cmds)
	}
	writeLaunchJSON(w, out)
}

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type newChatRequest struct {
	Window    string `json:"window"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	Profile   string `json:"profile"`
	Workspace string `json:"workspace"`
	Isolate   bool   `json:"isolate"`
	Bot       string `json:"bot"`
	// Agent opens this harness (claude, codex) instead of the
	// workspace's first; absent means the workspace decides.
	Agent string `json:"agent"`
}

func launchNewHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {window, prompt, model, profile} to open a chat")
		return
	}
	var req newChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	agent, cmd, no := newChatCommand(req)
	if no != nil {
		writeLaunchError(w, no.status, no.msg)
		return
	}
	launchBoardCommand(w, command.Command{Action: command.ActionNew, WindowID: strings.TrimSpace(req.Window), Command: daemon.NewSessionCommandFor(agent, cmd...), Source: "phone"})
}

func newChatCommand(req newChatRequest) (string, []string, *launchRefusal) {
	model, profile, no := launchModelAndProfile(req.Model, req.Profile)
	if no != nil {
		return "", nil, no
	}
	agent := strings.ToLower(strings.TrimSpace(req.Agent))
	if agent != "" && !harness.Known(agent) {
		return "", nil, refuse(http.StatusBadRequest, "agent is one of "+strings.Join(harness.Names(), ", "))
	}
	dir, err := agentDir()
	if err != nil {
		return "", nil, refuse(http.StatusInternalServerError, err.Error())
	}
	if no := knownWindow(dir, strings.TrimSpace(req.Window)); no != nil {
		return "", nil, no
	}
	args, no := newChatTargets(req)
	if no != nil {
		return "", nil, no
	}
	args = append(args, sessionFlags{profile: profile, model: model, isolate: req.Isolate}.args()...)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		id, err := savePrompt(dir, prompt)
		if err != nil {
			return "", nil, refuse(http.StatusInternalServerError, err.Error())
		}
		args = append(args, "--prompt-id", id)
	}
	return agent, args, nil
}

func knownWindow(dir, window string) *launchRefusal {
	if window == "" {
		return nil
	}
	rep, err := readBoard(dir)
	if err != nil {
		return refuse(http.StatusInternalServerError, err.Error())
	}
	if !windowConnected(rep, window) {
		return refuse(http.StatusNotFound, "that editor window is not connected any more")
	}
	return nil
}

func newChatTargets(req newChatRequest) ([]string, *launchRefusal) {
	args := []string{}
	if bot := strings.TrimSpace(req.Bot); bot != "" {
		if !bots.ValidName(bot) {
			return nil, refuse(http.StatusBadRequest, "not a bot name")
		}
		if _, err := loadBot(bot); err != nil {
			return nil, refuse(http.StatusNotFound, err.Error())
		}
		args = append(args, "--bot", bot)
	}
	if ws := strings.TrimSpace(req.Workspace); ws != "" {
		if _, err := workspaceRoot(ws); err != nil {
			return nil, refuse(http.StatusNotFound, err.Error())
		}
		args = append(args, "--workspace", ws)
	}
	return args, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

const watchBatchMax = 10

func addRunRecord(out map[string]any, rec watch.FixRecord) {
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
}

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
		if rec.Key == key {
			addRunRecord(out, rec)
			break
		}
	}
	log := daemon.RunLog(dir, key, tail)
	if _, known := out["ref"]; !known && log == "" {
		writeLaunchError(w, http.StatusNotFound, "no run for that key")
		return
	}
	out["log"] = log
	writeLaunchJSON(w, out)
}

func launchPushHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, agentDirUnresolvedMessage)
		return
	}
	device, ok := authorizedDevice(pairing.StorePath(dir), r.Header.Get("Authorization"))
	if !ok || device == "" {
		writeLaunchError(w, http.StatusForbidden, "push tokens belong to a paired device; pair the phone first")
		return
	}
	store := push.Load(dir)
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Token string `json:"token"`
			Quiet string `json:"quiet"`
			Only  string `json:"only"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the token")
			return
		}
		if err := store.SetWith(device, req.Token, push.Prefs{Quiet: strings.TrimSpace(req.Quiet), Only: strings.TrimSpace(req.Only)}); err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLaunchJSON(w, map[string]any{"done": "this phone gets notified", "device": device, "quiet": req.Quiet, "only": req.Only})
	case http.MethodDelete:
		_ = store.Remove(device)
		writeLaunchJSON(w, map[string]any{"done": "this phone is quiet", "device": device})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {token} to register, DELETE to stop")
	}
}

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
	writeLaunchJSON(w, map[string]any{"columns": kanbanColumns, "cards": cards, "boards": boards, "features": kanbanFeatures(cards)})
}

type inboxRow struct {
	Key        string            `json:"key"`
	Kind       string            `json:"kind"`
	Ref        string            `json:"ref"`
	Title      string            `json:"title"`
	URL        string            `json:"url,omitempty"`
	Workspace  string            `json:"workspace,omitempty"`
	At         time.Time         `json:"at"`
	Actionable bool              `json:"actionable"`
	State      string            `json:"state,omitempty"`
	Blocked    string            `json:"blocked,omitempty"`
	BlockedBy  string            `json:"blockedBy,omitempty"`
	Priority   int               `json:"priority"`
	Author     string            `json:"author,omitempty"`
	Body       string            `json:"body,omitempty"`
	Session    *CardSess         `json:"session,omitempty"`
	Picked     *CardPick         `json:"picked,omitempty"`
	Columns    []string          `json:"columns,omitempty"`
	PR         string            `json:"pr,omitempty"`
	Pull       *watch.PullStatus `json:"pull,omitempty"`
	Handed     *watch.Hand       `json:"handed,omitempty"`
	Standing   sessions.Standing `json:"standing"`
}

type inboxSources struct {
	dir      string
	onTicket map[string]*CardSess
	picks    *watch.PickLog
	pulls    *watch.PullLog
	hands    *watch.HandLog
	moved    *watch.StateLog
	state    *watch.State
	fixLog   *watch.FixLog
	doneLog  *watch.DoneLog
	now      time.Time
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
	src := inboxSources{dir: dir, onTicket: sessionsOnTickets(dir), picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir),
		hands: watch.LoadHands(dir), moved: watch.LoadStateLog(dir), state: watch.LoadState(dir), fixLog: watch.LoadFixLog(dir),
		doneLog: watch.LoadDoneLog(dir), now: time.Now()}
	out := inboxRows(src)
	sortInboxRows(out)
	writeLaunchJSON(w, map[string]any{"events": out, "fixes": recentFixRows(dir, src.fixLog), "boards": inboxBoards(dir, out)})
}

func inboxRows(src inboxSources) []inboxRow {
	out := []inboxRow{}
	keeper := watch.NewInboxKeeper(time.Now())
	for _, e := range watch.RecentEvents(src.dir, inboxWindow) {
		if !keeper.Keep(e) || src.state.IsIgnored(e.Key) {
			continue
		}
		current := e.State
		if now, ok := src.moved.Get(e.Key); ok {
			current = now.Status
		}
		if watch.InboxHandled(e, current, src.pulls, src.fixLog, src.doneLog) != "" {
			continue
		}
		out = append(out, inboxRowFor(src, e, current))
	}
	return out
}

func inboxRowFor(src inboxSources, e watch.Event, current string) inboxRow {
	r := inboxRow{Key: e.Key, Kind: string(e.Kind), Ref: e.Ref, Title: firstLineOf(e.Title),
		URL: e.URL, Workspace: e.Workspace, At: e.At, Actionable: daemon.FixPrompt(e) != "", State: current, Priority: watch.Priority(e),
		Author: e.Author, Body: e.Body}
	if b, ok := src.fixLog.Blocked(e.Workspace, e.Ref); ok {
		r.Blocked, r.BlockedBy = b.Reason, b.By
	}
	r.Session = src.onTicket[strings.ToLower(e.Ref)]
	if p, ok := src.picks.Get(e.Key); ok && src.now.Sub(p.At) <= watch.PickFresh {
		r.Picked = &CardPick{At: p.At, By: p.By}
	}
	if e.Kind == watch.KindTask {
		r.Columns = watch.TaskColumns
	}
	r.PR = prLinkFor(src.dir, e, src.onTicket)
	if st, ok := src.pulls.Get(firstNonEmptyString(r.PR, e.Ref)); ok {
		r.Pull = &st
	}
	if h, ok := src.hands.Get(e.Key); ok {
		r.Handed = &h
	}
	r.Standing = rowStanding(r.Pull, r.PR, r.Blocked, r.Session)
	return r
}

func recentFixRows(dir string, fixLog *watch.FixLog) []map[string]any {
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
		if e, ok := watch.FindEvent(dir, r.Key); ok && e.URL != "" {
			row["url"] = e.URL
		}
		fixes = append(fixes, row)
	}
	return fixes
}

func inboxBoards(dir string, out []inboxRow) map[string]any {
	boards := map[string]any{}
	cache := watch.LoadBoardCache(dir)
	seen := map[string]bool{}
	for _, e := range out {
		if e.Workspace == "" || seen[e.Workspace] {
			continue
		}
		seen[e.Workspace] = true
		info := cache.Get(e.Workspace)
		if !info.Has() {
			continue
		}
		names := make([]string, 0, len(info.Statuses))
		for _, st := range info.Statuses {
			names = append(names, st.Name)
		}
		boards[e.Workspace] = map[string]any{"columns": names, "me": info.Me.Name}
	}
	return boards
}

func sortInboxRows(out []inboxRow) {
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
}

type ticketRequest struct {
	Key       string   `json:"key"`
	Do        string   `json:"do"`
	Status    string   `json:"status"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Workspace string   `json:"workspace"`
	Ref       string   `json:"ref"`
	Links     []string `json:"links"`
	Owner     string   `json:"owner"`
	Force     bool     `json:"force"`
	Priority  *string  `json:"priority"`
	Due       *string  `json:"due"`
	BlockedBy []string `json:"blockedBy"`
}

type ticketCall struct {
	w     http.ResponseWriter
	r     *http.Request
	dir   string
	event watch.Event
	req   ticketRequest
}

func (c ticketCall) from() string {
	return firstNonEmptyString(c.r.URL.Query().Get("from"), "phone")
}

func (c ticketCall) fail(status int, msg string) {
	writeLaunchError(c.w, status, msg)
}

func (c ticketCall) done(out map[string]any) {
	writeLaunchJSON(c.w, out)
}

var rowActions = map[string]func(ticketCall){
	"ignore":   ignoreRow,
	"handover": handOverRow,
	"done":     markRowDone,
	"unblock":  unblockRow,
}

func launchTicketHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {key, do, status} to change a ticket")
		return
	}
	var req ticketRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	event, ok := ticketEventFor(dir, req)
	if !ok {
		writeLaunchError(w, http.StatusNotFound, "no such watch event")
		return
	}
	call := ticketCall{w: w, r: r, dir: dir, event: event, req: req}
	do := strings.TrimSpace(req.Do)
	if act, ok := rowActions[do]; ok {
		act(call)
		return
	}
	if event.Kind == watch.KindTask {
		taskRowAction(call, do)
		return
	}
	trackerRowAction(call, do)
}

func ticketEventFor(dir string, req ticketRequest) (watch.Event, bool) {
	if key := strings.TrimSpace(req.Key); key != "" {
		return watch.FindEvent(dir, key)
	}
	return watch.FindEventByRef(dir, strings.TrimSpace(req.Ref), strings.TrimSpace(req.Workspace))
}

func ignoreRow(c ticketCall) {
	_ = watch.LoadState(c.dir).Ignore(c.event.Key)
	c.done(map[string]any{"done": "ignored " + firstNonEmptyString(c.event.Ref, c.event.Key)})
}

func handOverRow(c ticketCall) {
	line := watch.HandoverLine(c.event)
	if line == "" {
		c.fail(http.StatusBadRequest, "nothing to hand over on a "+string(c.event.Kind))
		return
	}
	target := sessionsOnTickets(c.dir)[strings.ToLower(c.event.Ref)]
	if target == nil {
		target = sessionOnPull(c.dir, watch.PullLinkOf(c.event))
	}
	if target == nil {
		c.fail(http.StatusNotFound, "no live session is on "+c.event.Ref+" - Work on it opens one")
		return
	}
	from := c.from()
	_ = watch.LoadHands(c.dir).Set(c.event.Key, watch.Hand{At: time.Now(), To: target.ID, Label: target.Label, By: from})
	launchBoardCommand(c.w, command.Command{Action: command.ActionSend, SessionID: target.ID, Text: line, Enter: true, Source: from})
}

func sessionOnPull(dir, link string) *CardSess {
	if link == "" {
		return nil
	}
	rep, err := readBoard(dir)
	if err != nil {
		return nil
	}
	for _, s := range rep.Sessions {
		if s.Status != sessions.StatusGone && s.Status != sessions.StatusStale && s.PR != "" && strings.HasPrefix(s.PR, link) {
			return &CardSess{ID: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status)}
		}
	}
	return nil
}

// markRowDone: this row is handled on this side: a reply went out, the
// branch was pushed, it was read. Later rows on the same ticket come in as usual.
func markRowDone(c ticketCall) {
	if err := watch.LoadDoneLog(c.dir).MarkKey(c.event.Key, time.Now(), c.from()); err != nil {
		c.fail(http.StatusInternalServerError, err.Error())
		return
	}
	c.done(map[string]any{"done": c.event.Ref + " handled"})
}

func unblockRow(c ticketCall) {
	if !watch.LoadFixLog(c.dir).Unblock(c.event.Workspace, c.event.Ref) {
		c.fail(http.StatusBadRequest, c.event.Ref+" is not blocked")
		return
	}
	if _, _, err := watchWriter(c.dir, c.event.Workspace); err == nil {
		go writeWorkpad(c.dir, c.event.Workspace, c.event.Ref, "Blocked", "")
	}
	c.done(map[string]any{"done": "unblocked " + c.event.Ref})
}

func taskRowAction(c ticketCall, do string) {
	tasks := watch.LoadTasks(c.dir)
	now := time.Now()
	var t watch.Task
	var err error
	var out func(watch.Task) map[string]any
	switch do {
	case "move":
		t, err = tasks.Move(c.event.Key, c.req.Status, now)
		out = func(t watch.Task) map[string]any {
			return map[string]any{"done": t.Ref() + " → " + t.State, "state": t.State}
		}
	case "close":
		t, err = tasks.Move(c.event.Key, "Done", now)
		out = func(t watch.Task) map[string]any { return map[string]any{"done": t.Ref() + " done", "state": t.State} }
	case "edit":
		t, err = tasks.Edit(c.event.Key, c.req.Title, c.req.Body, c.req.Workspace, now)
		out = func(t watch.Task) map[string]any { return map[string]any{"done": t.Ref() + " changed"} }
	case "remove":
		t, err = tasks.Remove(c.event.Key)
		if err == nil {
			_ = watch.LoadState(c.dir).Ignore(c.event.Key)
		}
		out = func(t watch.Task) map[string]any { return map[string]any{"done": t.Ref() + " removed"} }
	case "link":
		t, err = tasks.Link(c.event.Key, c.req.Links, now)
		out = func(t watch.Task) map[string]any {
			return map[string]any{"done": t.Ref() + " linked", "links": t.LinkRows()}
		}
	case "unlink":
		for _, link := range c.req.Links {
			if t, err = tasks.Unlink(c.event.Key, link, now); err != nil {
				break
			}
		}
		out = func(t watch.Task) map[string]any {
			return map[string]any{"done": t.Ref() + " unlinked", "links": t.LinkRows()}
		}
	case "assign":
		c.done(map[string]any{"done": c.event.Ref + " is yours already"})
		return
	case "claim":
		t, err = tasks.Claim(c.event.Key, c.claimant(), c.req.Force, now)
		out = func(t watch.Task) map[string]any {
			return map[string]any{"done": t.Ref() + " is " + t.Owner + "'s", "owner": t.Owner}
		}
	case "release":
		t, err = tasks.Release(c.event.Key, now)
		out = func(t watch.Task) map[string]any { return map[string]any{"done": t.Ref() + " is free"} }
	case "plan":
		t, err = planTaskRow(tasks, c, now)
		out = func(t watch.Task) map[string]any {
			return map[string]any{"done": t.Ref() + " planned", "priority": t.Priority, "due": t.Due, "blockedBy": t.BlockedBy}
		}
	default:
		c.fail(http.StatusBadRequest, "a task takes move, close, edit, remove, link, unlink, claim, release, plan or ignore")
		return
	}
	if err != nil {
		c.fail(http.StatusBadRequest, err.Error())
		return
	}
	c.done(out(t))
}

func (c ticketCall) claimant() string {
	if who := strings.TrimSpace(c.req.Owner); who != "" {
		return who
	}
	if device, ok := authorizedDeviceFull(pairing.StorePath(c.dir), c.r.Header.Get("Authorization")); ok && device.Name != "" {
		return device.Name
	}
	return c.from()
}

func planTaskRow(tasks *watch.TaskLog, c ticketCall, now time.Time) (watch.Task, error) {
	t, ok := tasks.Find(c.event.Key)
	if !ok {
		return t, fmt.Errorf("no task %s", c.event.Ref)
	}
	var err error
	if c.req.Priority != nil {
		if t, err = tasks.SetPriority(c.event.Key, *c.req.Priority, now); err != nil {
			return t, err
		}
	}
	if c.req.Due != nil {
		if t, err = tasks.SetDue(c.event.Key, *c.req.Due, now); err != nil {
			return t, err
		}
	}
	if c.req.BlockedBy != nil {
		t, err = tasks.SetBlockedBy(c.event.Key, c.req.BlockedBy, now)
	}
	return t, err
}

func trackerRowAction(c ticketCall, do string) {
	if c.event.Workspace == "" {
		c.fail(http.StatusBadRequest, "that event belongs to no workspace")
		return
	}
	writer, _, err := watchWriter(c.dir, c.event.Workspace)
	if err != nil {
		c.fail(http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.r.Context(), 30*time.Second)
	defer cancel()
	switch do {
	case "move":
		moveTrackerRow(ctx, c, writer)
	case "merge", "close", "ready", "draft", "reopen":
		changePullOnRow(ctx, c, do)
	case "approve", "request", "comment":
		reviewPullOnRow(ctx, c, do)
	case "assign":
		assignTrackerRow(ctx, c, writer)
	default:
		c.fail(http.StatusBadRequest, "do is move, assign, merge, close, ready, draft, reopen, approve, request, comment, ignore or unblock")
	}
}

func moveTrackerRow(ctx context.Context, c ticketCall, writer watch.Writer) {
	ref := strings.TrimSpace(c.event.Ref)
	status := strings.TrimSpace(c.req.Status)
	if status == "" {
		c.fail(http.StatusBadRequest, "name the column to move it to")
		return
	}
	if err := writer.Move(ctx, ref, status); err != nil {
		c.fail(http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	_ = watch.LoadStateLog(c.dir).SetFrom(c.event.Key, status, c.event.State, time.Now())
	c.done(map[string]any{"done": ref + " → " + status, "state": status})
}

func pullChange(ctx context.Context, secrets watch.Secrets, link, do string) (did, state string, err error) {
	switch do {
	case "merge":
		return "merged ", "merged", watch.MergePR(ctx, secrets, link)
	case "ready":
		return "ready for review: ", "open", watch.ReadyPR(ctx, secrets, link)
	case "draft":
		return "back to draft: ", "draft", watch.DraftPR(ctx, secrets, link)
	case "reopen":
		return "reopened ", "open", watch.ReopenPR(ctx, secrets, link)
	}
	return "closed ", "closed", watch.ClosePR(ctx, secrets, link)
}

func changePullOnRow(ctx context.Context, c ticketCall, do string) {
	link := prLinkFor(c.dir, c.event, nil)
	if link == "" {
		c.fail(http.StatusBadRequest, "no pull request of yours on this")
		return
	}
	did, state, err := pullChange(ctx, watch.LoadSecretsFor(c.dir, c.event.Workspace), link, do)
	if err != nil {
		c.fail(http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	if k := c.event.Kind; strings.HasPrefix(string(k), "pr.") || k == watch.KindCIFailed || k == watch.KindReviewRequested {
		_ = watch.LoadStateLog(c.dir).SetFrom(c.event.Key, state, c.event.State, time.Now())
	}
	c.done(map[string]any{"done": did + link, "url": link, "state": state})
}

func reviewPullOnRow(ctx context.Context, c ticketCall, do string) {
	link := c.event.URL
	if watch.PullRef(link) == "" {
		link = prLinkFor(c.dir, c.event, nil)
	}
	if link == "" {
		c.fail(http.StatusBadRequest, "no pull request on this row")
		return
	}
	if err := watch.ReviewPR(ctx, watch.LoadSecretsFor(c.dir, c.event.Workspace), link, c.req.Do, c.req.Body); err != nil {
		c.fail(http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	did := map[string]string{"approve": "approved ", "request": "changes requested on ", "comment": "commented on "}[do]
	if c.event.Kind == watch.KindReviewRequested && c.req.Do != "comment" {
		_ = watch.LoadStateLog(c.dir).SetFrom(c.event.Key, "reviewed", c.event.State, time.Now())
	}
	c.done(map[string]any{"done": did + link, "url": link})
}

func assignTrackerRow(ctx context.Context, c ticketCall, writer watch.Writer) {
	ref := strings.TrimSpace(c.event.Ref)
	me := watch.LoadBoardCache(c.dir).Get(c.event.Workspace).Me
	if me.ID == "" {
		got, err := writer.Whoami(ctx)
		if err != nil {
			c.fail(http.StatusBadGateway, firstLineOf(err.Error()))
			return
		}
		me = got
	}
	if err := writer.Assign(ctx, ref, me.ID); err != nil {
		c.fail(http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	c.done(map[string]any{"done": ref + " is yours"})
}

func prLinkFor(dir string, e watch.Event, onTicket map[string]*CardSess) string {
	if link := prCorgiOpened(dir, e.Key); link != "" {
		return link
	}
	if onTicket == nil {
		onTicket = sessionsOnTickets(dir)
	}
	if s := onTicket[strings.ToLower(e.Ref)]; s != nil && s.PR != "" {
		return s.PR
	}
	if e.Mine && (strings.HasPrefix(string(e.Kind), "pr.") || e.Kind == watch.KindCIFailed) {
		link := e.URL
		if i := strings.Index(link, "#"); i > 0 {
			link = link[:i]
		}
		if strings.Contains(link, "/pull/") || strings.Contains(link, "/-/merge_requests/") {
			return link
		}
	}
	return ""
}

func waitingRank(kind string) int {
	switch kind {
	case "review.requested":
		return 0
	case "ci.failed":
		return 1
	case "pr.review", "pr.comment":
		return 2
	case "issue.comment":
		return 3
	default:
		return 4
	}
}

func prCorgiOpened(agentD, key string) string {
	for _, r := range watch.LoadFixLog(agentD).RecentFixes("", 50) {
		if r.Key == key && len(r.PRs) > 0 {
			return r.PRs[0]
		}
	}
	return ""
}

func launchWorkOnHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {key, window, model, profile} to work on a watch event")
		return
	}
	var req struct {
		Key      string   `json:"key"`
		Keys     []string `json:"keys"`
		Window   string   `json:"window"`
		Model    string   `json:"model"`
		Profile  string   `json:"profile"`
		From     string   `json:"from"`
		Isolate  bool     `json:"isolate"`
		Attempts int      `json:"attempts"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	for _, m := range req.Models {
		if m != "" && !validModel(m) {
			writeLaunchError(w, http.StatusBadRequest, modelNameRuleMessage)
			return
		}
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
	source := "phone"
	if strings.TrimSpace(req.From) == "page" {
		source = "page"
	}
	cmds, status, msg := workOnCommands(dir, keys, workOnOptions{Window: req.Window, Model: req.Model, Profile: req.Profile, Source: source, Isolate: req.Isolate}, req.Attempts, req.Models)
	if status != 0 {
		writeLaunchError(w, status, msg)
		return
	}
	launchBoardCommands(w, cmds)
}

var ticketRefPattern = regexp.MustCompile(`^[A-Za-z0-9_./#!-]{1,120}$`)
var ticketKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:/#!@-]{1,200}$`)

type workOnOptions struct {
	Window, Model, Profile, Source string
	Isolate                        bool
	Attempt                        int
}

func workOnCommands(dir string, keys []string, opt workOnOptions, n int, models []string) ([]command.Command, int, string) {
	if n < 1 {
		n = 1
	}
	if n > maxAttempts {
		return nil, http.StatusBadRequest, fmt.Sprintf("at most %d attempts at once", maxAttempts)
	}
	var out []command.Command
	for i := 1; i <= n; i++ {
		o := opt
		if n > 1 {
			o.Attempt, o.Isolate = i, true
		}
		if len(models) > 0 {
			o.Model = strings.TrimSpace(models[(i-1)%len(models)])
		}
		c, status, msg := workOnCommand(dir, keys, o)
		if status != 0 {
			return nil, status, msg
		}
		out = append(out, c)
	}
	return out, 0, ""
}

const maxAttempts = 5

type launchRefusal struct {
	status int
	msg    string
}

func refuse(status int, msg string) *launchRefusal {
	return &launchRefusal{status: status, msg: msg}
}

func workOnCommand(dir string, keys []string, opt workOnOptions) (command.Command, int, string) {
	cmd, no := workOnCommandFor(dir, keys, opt)
	if no != nil {
		return command.Command{}, no.status, no.msg
	}
	return cmd, 0, ""
}

func workOnCommandFor(dir string, keys []string, opt workOnOptions) (command.Command, *launchRefusal) {
	events, no := workEvents(dir, keys)
	if no != nil {
		return command.Command{}, no
	}
	prompt := daemon.BatchPrompt(events)
	if prompt == "" {
		return command.Command{}, refuse(http.StatusConflict, "only new issues can be worked on together")
	}
	model, profile, no := launchModelAndProfile(opt.Model, opt.Profile)
	if no != nil {
		return command.Command{}, no
	}
	window, no := workWindow(dir, strings.TrimSpace(opt.Window), events[0].Workspace)
	if no != nil {
		return command.Command{}, no
	}
	id, err := savePrompt(dir, prompt)
	if err != nil {
		return command.Command{}, refuse(http.StatusInternalServerError, err.Error())
	}
	go markPickedUp(dir, events)
	recordWorkPicks(dir, events, opt.Source)
	args := workOnArgs(events, sessionFlags{profile: profile, model: model, isolate: opt.Isolate, attempt: opt.Attempt})
	args = append(args, "--prompt-id", id)
	return command.Command{Action: command.ActionNew, WindowID: window,
		Command: daemon.NewSessionCommand(args...), Source: opt.Source}, nil
}

func workEvents(dir string, keys []string) ([]watch.Event, *launchRefusal) {
	if len(keys) == 0 {
		return nil, refuse(http.StatusBadRequest, "name the event to work on")
	}
	if len(keys) > watchBatchMax {
		return nil, refuse(http.StatusBadRequest, "too many at once")
	}
	var events []watch.Event
	for _, key := range keys {
		event, ok := watch.FindEvent(dir, strings.TrimSpace(key))
		if !ok {
			return nil, refuse(http.StatusNotFound, "no such watch event")
		}
		if len(events) > 0 && event.Workspace != events[0].Workspace {
			return nil, refuse(http.StatusBadRequest, "those are in different workspaces - take one workspace at a time")
		}
		events = append(events, event)
	}
	return events, nil
}

func launchModelAndProfile(rawModel, rawProfile string) (model, profile string, no *launchRefusal) {
	model = strings.TrimSpace(rawModel)
	if model != "" && !validModel(model) {
		return "", "", refuse(http.StatusBadRequest, modelNameRuleMessage)
	}
	profile = strings.TrimSpace(rawProfile)
	if profile == "" || profile == "default" {
		return model, "", nil
	}
	if !profileNamePattern.MatchString(profile) || !containsString(launchProfileNames(), profile) {
		return "", "", refuse(http.StatusBadRequest, noSuchProfileMessage)
	}
	return model, profile, nil
}

func workWindow(dir, window, workspace string) (string, *launchRefusal) {
	rep, boardErr := readBoard(dir)
	if window != "" {
		if boardErr != nil {
			return "", refuse(http.StatusInternalServerError, boardErr.Error())
		}
		if !windowConnected(rep, window) {
			return "", refuse(http.StatusNotFound, "that editor window is not connected any more")
		}
	}
	if boardErr == nil {
		if own := windowOnWorkspace(rep, dir, workspace); own != "" {
			return own, nil
		}
	}
	return window, nil
}

func recordWorkPicks(dir string, events []watch.Event, source string) {
	now := time.Now()
	picks := watch.LoadPicks(dir)
	tasks := watch.LoadTasks(dir)
	for _, e := range events {
		_ = picks.Set(e.Key, source, now)
		if e.Kind == watch.KindTask {
			_, _ = tasks.Move(e.Key, "Doing", now)
		}
	}
}

type sessionFlags struct {
	workspace string
	profile   string
	model     string
	isolate   bool
	attempt   int
}

func (f sessionFlags) args() []string {
	args := []string{}
	for _, kv := range [][2]string{{"--workspace", f.workspace}, {"--profile", f.profile}, {"--model", f.model}} {
		if kv[1] != "" {
			args = append(args, kv[0], kv[1])
		}
	}
	if f.isolate {
		args = append(args, "--isolate")
	}
	if f.attempt > 0 {
		args = append(args, "--attempt", strconv.Itoa(f.attempt))
	}
	return args
}

func workOnArgs(events []watch.Event, flags sessionFlags) []string {
	flags.workspace = strings.TrimSpace(events[0].Workspace)
	var refs []string
	for _, e := range events {
		if ref := strings.TrimSpace(e.Ref); ref != "" && ticketRefPattern.MatchString(ref) {
			refs = append(refs, ref)
		}
	}
	isolate, attempt := flags.isolate, flags.attempt
	flags.isolate, flags.attempt = false, 0
	args := flags.args()
	if len(refs) > 0 && ticketKeyPattern.MatchString(events[0].Key) {
		args = append(args, "--ticket", strings.Join(refs, ","), "--ticket-key", events[0].Key)
	}
	return append(args, sessionFlags{isolate: isolate, attempt: attempt}.args()...)
}

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
			if best == "" || win.FocusedAt.After(bestFocus) {
				best, bestFocus = win.ID, win.FocusedAt
			}
			break
		}
	}
	return best
}

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
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if clean := filepath.Clean(resolved); clean != forms[0] {
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

func sessionsOnTickets(dir string) map[string]*CardSess {
	out := map[string]*CardSess{}
	rep, err := readBoard(dir)
	if err != nil {
		return out
	}
	for _, s := range rep.Sessions {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		for _, ref := range sessionTicketRefs(s) {
			key := strings.ToLower(ref)
			if _, taken := out[key]; taken {
				continue
			}
			out[key] = &CardSess{ID: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status)}
		}
	}
	return out
}

// taskRunRefusal says why a task cannot start by itself in this workspace.
func taskRunRefusal(dir, ws string) string {
	if ws == "" {
		return "a task that starts by itself needs a workspace"
	}
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		return err.Error()
	}
	for _, s := range specs {
		if s.Workspace == ws && s.Action == "fix" {
			return ""
		}
	}
	return fmt.Sprintf("%s does not run tasks by itself yet: corgi agent level 3 --workspace %s", ws, ws)
}

// launchTaskCommentHandler adds to a task's thread and wakes a run where the
// workspace runs them.
func launchTaskCommentHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {key, text, by} to comment on a task")
		return
	}
	if isViewer(r) {
		writeLaunchError(w, http.StatusForbidden, "this device may only read")
		return
	}
	var req struct {
		Key  string `json:"key"`
		Text string `json:"text"`
		By   string `json:"by"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	by := strings.TrimSpace(req.By)
	if by == "" || by == watch.TaskAgent {
		by = "you"
	}
	t, err := watch.LoadTasks(dir).Comment(req.Key, by, req.Text, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	nudgePlans(dir)
	writeLaunchJSON(w, map[string]any{"done": "comment on " + t.Ref(), "ref": t.Ref(), "comments": t.Comments})
}

type addTaskRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Workspace string `json:"workspace"`
	Run       bool   `json:"run"`
	// The thread of a task another machine sends here.
	Comments []watch.TaskComment `json:"comments"`
}

func addTaskRefusal(dir string, r *http.Request, req addTaskRequest) *launchRefusal {
	if isViewer(r) {
		return refuse(http.StatusForbidden, "this device may only read")
	}
	if req.Run {
		if why := taskRunRefusal(dir, strings.TrimSpace(req.Workspace)); why != "" {
			return refuse(http.StatusBadRequest, why)
		}
	}
	if ws := strings.TrimSpace(req.Workspace); ws != "" {
		if _, err := workspaceRoot(ws); err != nil {
			return refuse(http.StatusBadRequest, "no such workspace: "+ws)
		}
	}
	return nil
}

func launchAddTask(w http.ResponseWriter, r *http.Request, dir string) {
	var req addTaskRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	if no := addTaskRefusal(dir, r, req); no != nil {
		writeLaunchError(w, no.status, no.msg)
		return
	}
	by := "phone"
	if device, ok := authorizedDeviceFull(pairing.StorePath(dir), r.Header.Get("Authorization")); ok && device.Peer() {
		by = device.Name
	}
	t, err := watch.LoadTasks(dir).Add(req.Title, req.Body, req.Workspace, by, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Comments) > 0 && len(req.Comments) <= 200 {
		t, _ = watch.LoadTasks(dir).WithThreadFrom(t.Ref(), req.Comments, time.Now())
	}
	if req.Run {
		t, _ = watch.LoadTasks(dir).AskRun(t.Ref(), time.Now())
		nudgePlans(dir)
	}
	writeLaunchJSON(w, map[string]any{"done": t.Ref() + " on the board", "key": t.Key(), "ref": t.Ref(), "task": t})
}

func launchTaskHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		tasks := watch.LoadTasks(dir).Events(time.Now())
		writeLaunchJSON(w, map[string]any{"tasks": tasks, "columns": watch.TaskColumns})
	case http.MethodPost:
		launchAddTask(w, r, dir)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the tasks, or POST {title, body, workspace} to add one")
	}
}
