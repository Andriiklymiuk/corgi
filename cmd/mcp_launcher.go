package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/usage"
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
	SessionLinks []string          `json:"sessionLinks,omitempty"`
	Note         string            `json:"note,omitempty"`
	Live         int               `json:"live"`
	TopSession   *launchTopSession `json:"topSession,omitempty"`
	Usage        *usage.Report     `json:"usage,omitempty"`
	LastEvent    *launchLastEvent  `json:"lastEvent,omitempty"`
	Profiles     []string          `json:"profiles,omitempty"`
}

type launchTopSession struct {
	Name      string `json:"name"`
	Where     string `json:"where"`
	StartedAt int64  `json:"startedAt,omitempty"`
	URL       string `json:"url,omitempty"`
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
	running  bool
	url      string
	note     string
	sessions []string
}

// buildLaunchWorkspaces joins the registry with the daemon's live status into
// the launcher rows. A start the daemon refused (sensitive, unknown profile,
// unreachable, bad bin) leaves a diagnostic warning, not a run state — merging
// it in is what stops the phone showing "Starting…" then silently giving up.
func buildLaunchWorkspaces(registry *workspace.Registry, status *daemon.Status) []launchWorkspace {
	running := map[string]wsRunState{}
	if status != nil {
		for _, ws := range status.Workspaces {
			running[ws.WorkspaceID] = wsRunState{running: ws.Running, url: ws.SessionURL, note: ws.LastReason, sessions: ws.Sessions}
		}
		for _, d := range status.Diagnostics {
			if d.Warning == "" {
				continue
			}
			s := running[d.WorkspaceID]
			s.note = d.Warning
			running[d.WorkspaceID] = s
		}
	}

	profiles := launchProfileNames()
	out := make([]launchWorkspace, 0, len(registry.Workspaces))
	for _, ws := range registry.Sorted() {
		s := running[ws.ID]
		row := launchWorkspace{
			ID: ws.ID, Aliases: ws.Aliases, Path: ws.AbsPath,
			Status: string(ws.Status), Running: s.running, SessionURL: s.url,
			SessionLinks: s.sessions, Note: s.note, Profiles: profiles,
		}
		row.Live, row.TopSession, row.LastEvent = workspaceActivity(ws.ID, ws.AbsPath)
		row.Usage = workspaceUsage(ws.ID, ws.AbsPath)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func workspaceActivity(id, absPath string) (int, *launchTopSession, *launchLastEvent) {
	// The pid-file reader, never listClaudeSessions: its fallback shells out,
	// and this runs per workspace on a list the phone polls while starting.
	var live int
	var top *launchTopSession
	if _, configDir, ok := workspaceSessionTarget(id); ok && absPath != "" {
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
			Name:      sessionDisplayName(sess),
			Where:     sessionWhereLabel(sess),
			StartedAt: sess.StartedAt,
			URL:       sess.URL,
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	return &launchTopSession{
		Name:      sessionDisplayName(sessions[0]),
		Where:     sessionWhereLabel(sessions[0]),
		StartedAt: sessions[0].StartedAt,
	}
}

func sessionDisplayName(sess claudeSession) string {
	if sess.Name != "" {
		return sess.Name
	}
	return "session"
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
func workspaceUsage(id, absPath string) *usage.Report {
	if absPath == "" {
		return nil
	}
	_, configDir, ok := workspaceSessionTarget(id)
	if !ok {
		return nil
	}

	usageCache.mu.Lock()
	entry := usageCache.reps[id]
	if entry == nil {
		entry = &cachedUsage{}
		usageCache.reps[id] = entry
	}
	fresh := !entry.computed.IsZero() && time.Since(entry.computed) < usageTTL
	report := entry.report
	if !fresh && !entry.refreshing {
		entry.refreshing = true
		go refreshUsage(id, absPath, expandTilde(configDir))
	}
	usageCache.mu.Unlock()
	return report
}

func refreshUsage(id, absPath, configDir string) {
	rep := usage.ForDir(absPath, configDir, mungeClaudeProjectDir(absPath), time.Now())
	usageCache.mu.Lock()
	defer usageCache.mu.Unlock()
	entry := usageCache.reps[id]
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
	Name            string `json:"name"`
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
	absPath, configDir, ok := workspaceSessionTarget(id)
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
func workspaceSessionTarget(id string) (absPath, configDir string, ok bool) {
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
	return ws.AbsPath, config.Resolve(id, repo, user).ConfigDir, true
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
		if !pidExists(cs.PID) {
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
<meta name="theme-color" content="#000000">
<title>corgi</title>
<style>
  /* One scale, one contrast ladder. Sizes are the ones a thumb and an arm's
     length of daylight need — this page is read on a phone, outdoors, in a
     hurry, so nothing here is smaller than 12px or shorter than 44px. */
  :root{
    --bg:#000;--surface:#101114;--surface2:#191a1e;--press:#212227;
    --line:#26272c;--hair:#1c1d21;
    --text:#fff;--dim:#a3a6ad;--dim2:#74777f;
    --green:#3ddc91;--amber:#ffb84d;--red:#ff6f61;
    --r:18px;--r-sm:12px;--pill:999px;
    --tap:48px;--pad:20px;
    --f-title:1.45rem;--f-row:1.06rem;--f-body:.95rem;--f-sub:.82rem;--f-cap:.75rem;
  }
  *{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
  /* Flex and grid beat the hidden attribute on specificity; this page toggles
     controls with .hidden, so it must not. */
  [hidden]{display:none!important}
  html{-webkit-text-size-adjust:100%}
  body{margin:0;background:var(--bg);color:var(--text);
      font-family:-apple-system,system-ui,"Segoe UI",Roboto,sans-serif;
      font-size:var(--f-body);line-height:1.45;-webkit-font-smoothing:antialiased;
      padding-bottom:calc(2rem + env(safe-area-inset-bottom))}
  h1,h2,h3{margin:0;letter-spacing:-.02em}
  button,input,select{font-family:inherit}
  button,a,summary,label,input,[role=button]{touch-action:manipulation}
  :focus-visible{outline:2px solid var(--text);outline-offset:2px;border-radius:6px}

  /* Header: stays put, so the machine you are driving is never off-screen. */
  header{position:sticky;top:0;z-index:20;padding:calc(.85rem + env(safe-area-inset-top)) var(--pad) .85rem;
      background:rgba(0,0,0,.78);backdrop-filter:blur(16px);-webkit-backdrop-filter:blur(16px);
      border-bottom:1px solid transparent;transition:border-color .2s}
  header.scrolled{border-bottom-color:var(--hair)}
  .bar{display:flex;align-items:center;gap:.75rem;max-width:34rem;margin:0 auto}
  .logo{width:2.5rem;height:2.5rem;border-radius:.85rem;background:var(--surface2);
      display:flex;align-items:center;justify-content:center;font-size:1.3rem;flex:0 0 auto}
  .bar .main{min-width:0;flex:1}
  h1{font-size:var(--f-title);font-weight:800;line-height:1.1}
  #host{display:block;color:var(--dim);font-size:var(--f-sub);margin-top:.1rem;line-height:1.35}
  #host .what{color:var(--dim);border-bottom:1px dotted var(--dim2);cursor:pointer}
  #host .down{color:var(--red)}
  .icon{width:var(--tap);height:var(--tap);border-radius:var(--pill);border:0;flex:0 0 auto;
      background:var(--surface2);color:var(--text);font-size:1.15rem;cursor:pointer;
      display:flex;align-items:center;justify-content:center}
  .icon:active{background:var(--press)}
  .icon.spin{animation:spin .8s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .hostnote{max-width:34rem;margin:.7rem auto 0;color:var(--dim);font-size:var(--f-sub);line-height:1.5}

  main{max-width:34rem;margin:0 auto;padding:.5rem var(--pad) 0}
  .sectionlabel{font-size:var(--f-cap);font-weight:700;letter-spacing:.09em;text-transform:uppercase;
      color:var(--dim2);margin:1.1rem .15rem .55rem}

  /* A workspace is one card: identity on the left, the one action on the
     right, everything else a tap into the sheet. */
  .ws{background:var(--surface);border-radius:var(--r);padding:.9rem;margin-bottom:.7rem;
      cursor:pointer;transition:background .12s}
  .ws:active{background:var(--surface2)}
  .row{display:flex;align-items:center;gap:.8rem}
  .ava{width:3rem;height:3rem;border-radius:.95rem;flex:0 0 auto;display:flex;align-items:center;
      justify-content:center;font-size:1.15rem;font-weight:700;color:#fff;letter-spacing:-.02em;
      background:linear-gradient(150deg,hsl(var(--h) 42% 34%),hsl(var(--h) 46% 20%))}
  .ws .main{min-width:0;flex:1}
  .name{font-size:var(--f-row);font-weight:700;letter-spacing:-.015em;
      white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .path{color:var(--dim2);font-size:var(--f-sub);margin-top:.05rem;
      font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
      white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .tags{display:flex;align-items:center;gap:.4rem;flex-wrap:wrap;margin-top:.35rem}
  .pill{display:inline-flex;align-items:center;gap:.3rem;height:1.4rem;padding:0 .5rem;
      border-radius:var(--pill);font-size:var(--f-cap);font-weight:700;letter-spacing:.01em;
      background:var(--surface2);color:var(--dim)}
  .pill.live{background:rgba(61,220,145,.14);color:var(--green)}
  .pill.warn{background:rgba(255,111,97,.14);color:var(--red)}
  .pill i{width:.4rem;height:.4rem;border-radius:50%;background:currentColor;
      animation:pulse 2s ease-in-out infinite}
  @keyframes pulse{50%{opacity:.35}}
  .why{color:var(--dim2);font-size:var(--f-sub)}
  .wnote{color:var(--red);font-size:var(--f-sub);line-height:1.5;margin-top:.6rem}

  /* Buttons: exactly one white one per card — Uber's rule, and the reason you
     never have to read a card twice to know what tapping it does. */
  .btn{height:2.65rem;min-width:5.2rem;padding:0 1.05rem;border:0;border-radius:var(--r-sm);
      font-size:var(--f-body);font-weight:700;letter-spacing:-.01em;cursor:pointer;flex:0 0 auto;
      display:inline-flex;align-items:center;justify-content:center;gap:.35rem;
      background:var(--surface2);color:var(--text);text-decoration:none;transition:transform .06s}
  .btn:active{transform:scale(.97)}
  .btn:disabled{opacity:.45}
  .btn.primary{background:#fff;color:#000}
  .btn.block{width:100%;height:3.4rem;font-size:1.02rem;margin-top:.7rem}
  .btn.danger{background:rgba(255,111,97,.13);color:var(--red)}
  .btn.quiet{background:none;color:var(--dim);font-weight:600}

  .cardfoot{display:flex;align-items:center;justify-content:space-between;gap:.6rem;width:100%;
      margin-top:.7rem;padding:.65rem 0 0;border:0;border-top:1px solid var(--hair);
      background:none;font-family:inherit;font-size:var(--f-sub);color:var(--dim2);
      text-align:left;cursor:pointer;min-height:2.4rem}
  .cardfoot .more{color:var(--dim);font-weight:600;flex:0 0 auto}
  .usage{font-variant-numeric:tabular-nums}

  /* The live session gets its own row: it is the thing you came here to open. */
  .top{display:flex;align-items:center;gap:.6rem;margin-top:.7rem;padding:.6rem .7rem;
      background:var(--surface2);border-radius:var(--r-sm);color:var(--text);
      text-decoration:none;min-height:2.9rem}
  .top .tmain{display:flex;flex-direction:column;min-width:0;flex:1}
  .top .tname{font-size:var(--f-body);font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .top .when{color:var(--dim2);font-size:var(--f-cap);margin-top:.05rem}
  .top .go{color:var(--dim);font-size:1.1rem;flex:0 0 auto}
  .top .go.small{font-size:var(--f-cap);color:var(--dim2)}
  .sdot{width:.45rem;height:.45rem;border-radius:50%;background:var(--green);flex:0 0 auto;
      animation:pulse 2s ease-in-out infinite}

  .intro{color:var(--dim);font-size:var(--f-body);line-height:1.55;margin:.2rem .15rem 1rem}
  .skel{background:var(--surface);border-radius:var(--r);height:5.6rem;margin-bottom:.7rem;
      background-image:linear-gradient(100deg,transparent 20%,rgba(255,255,255,.05) 40%,transparent 60%);
      background-size:220% 100%;animation:sweep 1.3s linear infinite}
  @keyframes sweep{to{background-position:-220% 0}}
  .msg{color:var(--dim);font-size:var(--f-body);line-height:1.55;margin:1rem .15rem}
  .empty{padding:2.4rem .15rem 1rem}
  .empty h2{font-size:1.35rem;font-weight:800;margin-bottom:.5rem}
  .empty p{color:var(--dim);font-size:var(--f-body);line-height:1.6;margin:0}
  .err{color:var(--red)}
  code{background:var(--surface2);padding:.12rem .4rem;border-radius:.4rem;font-size:.9em}

  /* Bottom sheet: every secondary control lives here, so the list stays one
     line of thought. Reachable with a thumb, dismissed with one. */
  body.locked{overflow:hidden}
  .sheet{position:fixed;inset:0;z-index:50;display:flex;align-items:flex-end}
  .sheet[hidden]{display:none}
  .scrim{position:absolute;inset:0;background:rgba(0,0,0,.65);opacity:0;transition:opacity .22s;border:0}
  .sheet.on .scrim{opacity:1}
  .panel{position:relative;width:100%;max-width:34rem;margin:0 auto;background:var(--surface);
      border-radius:1.4rem 1.4rem 0 0;max-height:90vh;overflow-y:auto;-webkit-overflow-scrolling:touch;
      padding:.4rem var(--pad) calc(1.1rem + env(safe-area-inset-bottom));
      transform:translateY(101%);transition:transform .26s cubic-bezier(.2,.8,.2,1)}
  .sheet.on .panel{transform:none}
  @media (prefers-reduced-motion:reduce){
    .panel,.scrim{transition:none}
    .skel,.sdot,.pill i,.icon.spin{animation:none}
  }
  .panel:focus,.panel:focus-visible{outline:none}
  .grab{display:block;width:2.4rem;height:.25rem;border-radius:var(--pill);background:#3a3c42;
      border:0;padding:0;margin:.5rem auto .9rem}
  .sheet h2{font-size:1.25rem;font-weight:800}
  .sheet .sub{color:var(--dim2);font-size:var(--f-sub);margin-top:.15rem;word-break:break-all;
      font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
  .srow{display:flex;align-items:center;gap:.8rem;width:100%;min-height:3.4rem;padding:.7rem 0;
      background:none;border:0;border-top:1px solid var(--hair);color:var(--text);
      font-size:var(--f-body);font-weight:600;text-align:left;cursor:pointer}
  .srow:first-of-type{margin-top:.9rem}
  .srow .sval{margin-left:auto;color:var(--dim);font-weight:500;flex:0 0 auto}
  .srow .go{color:var(--dim2);font-size:1.05rem;flex:0 0 auto}
  .srow.bad{color:var(--red)}
  .srow .desc{display:block;color:var(--dim2);font-size:var(--f-cap);font-weight:400;margin-top:.1rem}
  .field{margin-top:.9rem}
  .field label{display:block;font-size:var(--f-cap);font-weight:700;letter-spacing:.07em;
      text-transform:uppercase;color:var(--dim2);margin-bottom:.35rem}
  .field select,.field input{width:100%;height:3rem;background:var(--surface2);border:1px solid var(--line);
      color:var(--text);border-radius:var(--r-sm);padding:0 .8rem;font-size:var(--f-body)}
  .field input::placeholder{color:var(--dim2)}

  /* Session lists inside the sheet. */
  .grp{display:flex;align-items:center;gap:.6rem;margin:1.1rem 0 .2rem;color:var(--dim2);
      font-size:var(--f-cap);font-weight:700;letter-spacing:.09em;text-transform:uppercase}
  .grp i{flex:1;height:1px;background:var(--hair)}
  .s{display:flex;align-items:center;gap:.6rem;min-height:3rem;padding:.55rem 0;
      border-top:1px solid var(--hair);color:var(--text);text-decoration:none;font-size:var(--f-body)}
  .s .smain{display:flex;flex-direction:column;min-width:0;flex:1}
  .s .sname{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-weight:600}
  .s .when{color:var(--dim2);font-size:var(--f-cap);margin-top:.05rem}
  .s.past .sname{color:var(--dim);font-weight:500}
  .s .go{color:var(--dim2);flex:0 0 auto;font-size:1.05rem}
  .tag{font-size:.65rem;font-weight:700;color:var(--amber);border:1px solid rgba(255,184,77,.4);
      border-radius:.35rem;padding:.05rem .3rem;margin-left:.4rem;vertical-align:1px}
  .hint,.none{color:var(--dim2);font-size:var(--f-cap);line-height:1.5;padding:.45rem 0}
  .evrow{display:flex;justify-content:space-between;gap:.6rem;font-size:var(--f-sub);
      color:var(--dim2);padding:.35rem 0}
  .evrow b{font-weight:600;color:var(--dim);min-width:0}
  .evrow span{flex:0 0 auto;white-space:nowrap}

  /* Toast: a failure says so once, over the thumb, and gets out of the way. */
  .toast{position:fixed;left:50%;bottom:calc(1.2rem + env(safe-area-inset-bottom));transform:translate(-50%,1rem);
      z-index:60;max-width:calc(100% - 2.4rem);background:#fff;color:#000;font-size:var(--f-sub);
      font-weight:600;padding:.75rem 1rem;border-radius:var(--r-sm);opacity:0;
      transition:opacity .2s,transform .2s;box-shadow:0 .6rem 2rem rgba(0,0,0,.5)}
  .toast[hidden]{display:none}
  .toast.on{opacity:1;transform:translate(-50%,0)}
  .toast.bad{background:var(--red);color:#1a0503}

  /* Panels that are not the main job: quiet until opened. */
  details.card{background:var(--surface);border-radius:var(--r);padding:0 var(--pad);margin-top:.7rem}
  details.card summary{display:flex;align-items:center;justify-content:space-between;gap:.6rem;
      list-style:none;cursor:pointer;min-height:3.4rem;padding:.9rem 0;font-size:var(--f-body);font-weight:600}
  details.card summary::-webkit-details-marker{display:none}
  details.card summary .sh{color:var(--dim2);font-size:var(--f-sub);font-weight:400}
  details.card[open] summary .sh::after{content:" \2303"}
  details.card:not([open]) summary .sh::after{content:" \2304"}
  details.card[open]{padding-bottom:1.1rem}
  details.card h3{font-size:var(--f-cap);font-weight:700;letter-spacing:.09em;text-transform:uppercase;
      color:var(--dim2);margin:1.4rem 0 .5rem}
  details.card p{color:var(--dim);font-size:var(--f-sub);line-height:1.55;margin:.5rem 0}
  .tip{display:block;width:100%;text-align:left;background:none;border:0;border-top:1px solid var(--hair);
      padding:.9rem 0 .8rem;cursor:pointer;color:var(--text)}
  .tip-t{display:block;font-size:var(--f-body);font-weight:700}
  .tip-d{display:block;font-size:var(--f-sub);color:var(--dim);line-height:1.5;margin-top:.15rem}
  .tip-cmd{display:flex;align-items:center;justify-content:space-between;gap:.6rem;margin-top:.6rem;
      background:var(--surface2);border-radius:var(--r-sm);padding:.6rem .7rem;min-height:2.6rem}
  .tip-cmd code{min-width:0;overflow-x:auto;white-space:nowrap;background:none;padding:0;
      font-size:var(--f-sub);color:var(--text)}
  .tip-copy{font-size:var(--f-cap);font-weight:700;color:var(--dim2);flex:0 0 auto;letter-spacing:.04em}
  .tip.copied .tip-copy{color:var(--green)}
  .dev{display:flex;align-items:center;justify-content:space-between;gap:.6rem;
      padding:.75rem 0;border-top:1px solid var(--hair);font-size:var(--f-body)}
  .dev .sub{color:var(--dim2);font-size:var(--f-cap)}
  .chk{display:flex;gap:.6rem;font-size:var(--f-sub);padding:.7rem 0;border-top:1px solid var(--hair);line-height:1.5}
  .chk .mark{color:var(--green);flex:0 0 auto}
  .chk.bad .mark{color:var(--red)}
  .chk .fix{display:block;color:var(--dim2);font-size:var(--f-cap);margin-top:.15rem}
  .toggle{display:flex;align-items:center;gap:.7rem;min-height:var(--tap);color:var(--text);
      font-size:var(--f-body);cursor:pointer}
  .toggle input{accent-color:var(--green);width:1.2rem;height:1.2rem;margin:0}
  pre{background:#0a0b0d;border:1px solid var(--line);border-radius:var(--r-sm);padding:.8rem;
      overflow-x:auto;font-size:var(--f-cap);line-height:1.5;white-space:pre;color:var(--dim)}
  .foot{text-align:center;margin:1.6rem 0 0}
  .foot a{color:var(--text);font-size:var(--f-body);font-weight:600;text-decoration:none;
      display:inline-flex;align-items:center;min-height:var(--tap);padding:0 1rem}
</style>
<header id="top">
  <div class="bar">
    <span class="logo">🐕</span>
    <div class="main"><h1>corgi</h1><small id="host">your machine</small></div>
    <button class="icon" id="refresh" aria-label="Refresh" title="Refresh">&#x21bb;</button>
  </div>
  <p id="hostnote" class="hostnote" hidden></p>
</header>
<main>
  <div id="list"></div>

  <details class="card" id="tips" hidden>
    <summary><span>On the laptop</span><span class="sh">setup commands</span></summary>
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
      <span class="tip-d">Give a work repo its own account, then set its <b>Open links in</b> row to <b>chrome</b>.</span>
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
    <p class="hint" id="tipmsg">Tap a row to copy its command.</p>
  </details>

  <details class="card" id="settings" hidden>
    <summary><span>Settings</span><span class="sh">devices, doctor, connector</span></summary>

    <h3>Claude app</h3>
    <p>Add corgi as a custom connector on claude.ai (Settings → Connectors → Add custom) to drive this machine from the Claude app too.</p>
    <pre id="cfg"></pre>
    <button class="btn block" id="copycfg">Copy connector config</button>
    <p id="copymsg" class="hint"></p>

    <h3>This browser</h3>
    <p>Each card's <b>Open links in</b> row picks where session links open: <b>app</b> deep-links into the Claude app, <b>browser</b> keeps them here, <b>chrome</b> forces Chrome — the one to pick for a repo on a different Claude account. Remembered per workspace, here only.</p>
    <p><b>Hide</b> tucks a card away when you are showing this screen to someone. Hidden cards collapse into one button; nothing on the machine changes.</p>
    <label class="toggle"><input type="checkbox" id="showbridges"> Show hand-started (bridge) sessions</label>
    <p>A bridge is a session started on the laptop itself. Its claude.ai page shows only what you send from here, so it looks empty at first — the full transcript stays on the laptop.</p>

    <h3>Devices</h3>
    <p>Everything that scanned a pairing QR. Revoking one leaves the others working.</p>
    <div id="devices" class="hint">Loading…</div>

    <h3>If something will not start</h3>
    <p>The same checks as <code>corgi agent doctor</code>, run from here.</p>
    <button class="btn block" id="rundoctor">Run doctor</button>
    <div id="doctor"></div>
    <p>For a push to this phone when a session needs you, set <code>notifyUrl</code> in the agent config on the laptop. A Discord, Slack or Telegram webhook works as well as an ntfy topic — corgi picks the payload from the host, which matters because ntfy's iOS app is paid. Without it, notifications only reach the laptop.</p>
  </details>

  <p class="foot"><a id="allsessions" target="_blank" rel="noopener">See all your sessions on claude.ai ↗</a></p>
</main>

<div class="sheet" id="sheet" hidden>
  <button class="scrim" id="scrim" aria-label="Close"></button>
  <section class="panel" role="dialog" aria-modal="true" aria-labelledby="sheettitle" tabindex="-1">
    <button class="grab" id="grab" aria-label="Close"></button>
    <h2 id="sheettitle"></h2>
    <div class="sub" id="sheetsub"></div>
    <div id="sheetbody"></div>
  </section>
</div>
<div class="toast" id="toast" hidden></div>
<script>
  const esc = s => String(s).replace(/[&<>"']/g, c =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  // The session URL is scanned from process output; only ever click through to
  // a real https claude.ai link, never a non-https or off-domain target.
  const safeClaudeUrl = u => {
    try { const p = new URL(u); return p.protocol === 'https:' && (p.hostname === 'claude.ai' || p.hostname.endsWith('.claude.ai')); }
    catch { return false; }
  };
  const token = (() => { try { return localStorage.getItem('corgi_token') || ''; } catch { return ''; } })();
  const list = document.getElementById('list');
  const auth = { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json', 'ngrok-skip-browser-warning': '1' };
  // Set in JS (not a static href) so the page source carries no external link;
  // this is a user navigation to the Claude session list, not a loaded asset.
  try { document.getElementById('allsessions').href = 'https://claude.ai/code'; } catch {}

  let lastWorkspaces = [];
  const openMode = id => { try { return localStorage.getItem('corgi_open_' + id) || 'app'; } catch { return 'app'; } };
  const setOpenMode = (id, m) => { try { localStorage.setItem('corgi_open_' + id, m); } catch {} };
  const showBridges = () => { try { return localStorage.getItem('corgi_show_bridges') !== '0'; } catch { return true; } };
  const setShowBridges = on => { try { localStorage.setItem('corgi_show_bridges', on ? '1' : '0'); } catch {} };
  const hidden = () => { try { return new Set(JSON.parse(localStorage.getItem('corgi_hidden') || '[]')); } catch { return new Set(); } };
  const setHidden = s => { try { localStorage.setItem('corgi_hidden', JSON.stringify([...s])); } catch {} };
  const toggleHidden = id => { const h = hidden(); h.has(id) ? h.delete(id) : h.add(id); setHidden(h); render(lastWorkspaces); };
  let revealHidden = false;

  // One failure, one line, over the thumb — never a red block that pushes the
  // card you were aiming at somewhere else.
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
      setTimeout(() => { el.hidden = true; }, 220);
    }, 3600);
  }

  addEventListener('scroll', () => {
    document.getElementById('top').classList.toggle('scrolled', scrollY > 4);
  }, { passive: true });

  if (!token) {
    list.className = 'empty';
    list.innerHTML = '<h2>Pair this browser</h2><p>On the laptop run <code>corgi agent up</code> ' +
      'and scan the QR it prints. Then this page lists that machine&#39;s repos and starts ' +
      'Claude Code sessions on them.</p>';
    document.getElementById('refresh').hidden = true;
  } else {
    initSheet();
    initSettings();
    loadInfo();
    skeleton();
    load();
    document.getElementById('refresh').onclick = () => {
      const b = document.getElementById('refresh');
      b.classList.add('spin');
      Promise.all([load(), loadInfo()]).finally(() => setTimeout(() => b.classList.remove('spin'), 400));
    };
  }

  function skeleton() {
    list.className = '';
    list.innerHTML = '<div class="skel"></div><div class="skel"></div><div class="skel"></div>';
  }

  function initSettings() {
    const s = document.getElementById('settings');
    const connector = JSON.stringify({ mcpServers: { corgi: {
      url: location.origin + '/mcp', headers: { Authorization: 'Bearer ' + token } } } }, null, 2);
    document.getElementById('cfg').textContent = connector;
    s.hidden = false;
    document.getElementById('copycfg').onclick = async () => {
      const msg = document.getElementById('copymsg');
      try { await navigator.clipboard.writeText(connector); msg.textContent = '✓ Copied'; toast('Connector config copied'); }
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
    try { box.open = localStorage.getItem('corgi_tips_open') === '1'; } catch {}
    box.addEventListener('toggle', () => {
      try { localStorage.setItem('corgi_tips_open', box.open ? '1' : '0'); } catch {}
    });
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
      const devices = j.devices || [];
      if (!devices.length) { box.className = 'hint'; box.textContent = 'No devices paired yet.'; return; }
      box.className = ''; box.innerHTML = '';
      for (const d of devices) {
        const row = document.createElement('div');
        row.className = 'dev';
        const left = document.createElement('div');
        left.innerHTML = esc(d.name) + (d.current ? ' <span class="sub">· this device</span>' : '') +
          '<div class="sub">paired ' + esc(fmtWhen(d.pairedAt)) + '</div>';
        row.appendChild(left);
        if (!d.current) {
          const b = document.createElement('button');
          b.className = 'btn danger'; b.textContent = 'Revoke';
          b.onclick = () => revokeDevice(d.name, b);
          row.appendChild(b);
        }
        box.appendChild(row);
      }
    } catch (e) { box.className = 'hint err'; box.textContent = '✗ ' + e.message; }
  }

  async function revokeDevice(name, btn) {
    btn.disabled = true; btn.textContent = 'Revoking…';
    try {
      const r = await fetch('/launch/devices?name=' + encodeURIComponent(name), { method: 'DELETE', headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      toast('Revoked ' + name);
      loadDevices();
    } catch (e) { btn.disabled = false; btn.textContent = 'Revoke'; toast(e.message, true); }
  }

  async function runDoctor() {
    const box = document.getElementById('doctor');
    box.className = 'hint'; box.textContent = 'Checking…';
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
    } catch (e) { box.className = 'hint err'; box.textContent = '✗ ' + e.message; }
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
        if (i) el.appendChild(document.createTextNode(' · '));
        if (bit.indexOf('daemon ') === 0) {
          const tap = document.createElement('span');
          tap.className = 'what' + (j.daemon ? '' : ' down'); tap.textContent = bit;
          tap.onclick = () => toggleDaemonNote(j.daemon);
          el.appendChild(tap);
          return;
        }
        el.appendChild(document.createTextNode(bit));
      });
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

  // A stable colour per repo: the card you want is found by shape before it is
  // found by reading, which is the whole point of an avatar.
  function hue(id) {
    let h = 0;
    for (const ch of String(id)) h = (h * 31 + ch.charCodeAt(0)) % 360;
    return h;
  }

  function render(workspaces) {
    lastWorkspaces = workspaces;
    if (!workspaces.length) {
      list.className = 'empty';
      list.innerHTML = '<h2>No repos yet</h2><p>On the laptop run <code>corgi agent scan ~/dev</code> ' +
        'to register the stacks it finds, then pull this page down to refresh.</p>';
      return;
    }
    list.className = '';
    list.innerHTML = '';
    const intro = introLine(workspaces);
    if (intro) list.appendChild(intro);
    const label = document.createElement('div');
    label.className = 'sectionlabel';
    label.textContent = workspaces.length + (workspaces.length === 1 ? ' repo' : ' repos') + ' on this machine';
    list.appendChild(label);

    const hide = hidden();
    const shown = revealHidden ? workspaces : workspaces.filter(w => !hide.has(w.id));
    const hiddenCount = workspaces.length - shown.length;
    for (const ws of shown) list.appendChild(card(ws, hide));
    if (hiddenCount || revealHidden) {
      const b = document.createElement('button');
      b.className = 'btn quiet block';
      b.textContent = revealHidden ? 'Hide those again' : hiddenCount + ' hidden — show';
      b.onclick = () => { revealHidden = !revealHidden; render(lastWorkspaces); };
      list.appendChild(b);
    }
  }

  function card(ws, hide) {
    const el = document.createElement('article');
    el.className = 'ws';
    // Tap anywhere that is not itself a control; the footer button below is the
    // same door, and the one a keyboard or screen reader can find.
    el.onclick = () => openSheet(ws);

    const row = document.createElement('div');
    row.className = 'row';
    const ava = document.createElement('span');
    ava.className = 'ava';
    ava.style.setProperty('--h', hue(ws.id));
    ava.textContent = String(ws.id || '?').replace(/[^a-zA-Z0-9]/g, '').slice(0, 1).toUpperCase() || '?';
    ava.setAttribute('aria-hidden', 'true');
    row.appendChild(ava);

    const main = document.createElement('div');
    main.className = 'main';
    const name = document.createElement('div');
    name.className = 'name'; name.textContent = ws.id;
    main.appendChild(name);
    const path = document.createElement('div');
    path.className = 'path'; path.textContent = shortPath(ws.path);
    main.appendChild(path);
    const tags = statusTags(ws);
    if (tags) main.appendChild(tags);
    row.appendChild(main);
    row.appendChild(primaryAction(ws));
    el.appendChild(row);

    if (!ws.running && ws.note) {
      const note = document.createElement('div');
      note.className = 'wnote'; note.textContent = ws.note;
      el.appendChild(note);
    }
    const top = topSessionRow(ws);
    if (top) el.appendChild(top);

    const foot = document.createElement('button');
    foot.className = 'cardfoot';
    foot.setAttribute('aria-label', ws.id + ' — sessions and options');
    const left = document.createElement('span');
    left.className = 'usage';
    left.textContent = usageText(ws) || (hide.has(ws.id) ? 'hidden on this browser' : sessionsHint(ws));
    const more = document.createElement('span');
    more.className = 'more';
    more.textContent = 'Details ›';
    foot.appendChild(left); foot.appendChild(more);
    el.appendChild(foot);
    return el;
  }

  // The one white button on the card. Open when there is something to open,
  // Start when there is not — never both, never neither.
  function primaryAction(ws) {
    if (ws.sessionUrl && safeClaudeUrl(ws.sessionUrl)) {
      const mode = openMode(ws.id);
      if (mode === 'browser' || mode === 'chrome') {
        const b = document.createElement('button');
        b.className = 'btn primary'; b.textContent = 'Open';
        b.onclick = (e) => {
          e.stopPropagation();
          if (mode === 'chrome') { location.href = chromeUrl(ws.sessionUrl); }
          else { window.open(ws.sessionUrl, '_blank', 'noopener'); }
        };
        return b;
      }
      // app mode uses a real anchor tap so iOS deep-links into the Claude app.
      const a = document.createElement('a');
      a.className = 'btn primary'; a.href = ws.sessionUrl; a.textContent = 'Open';
      a.target = '_blank'; a.rel = 'noopener noreferrer';
      a.onclick = (e) => e.stopPropagation();
      return a;
    }
    const b = document.createElement('button');
    b.className = 'btn primary';
    b.textContent = ws.running ? 'Starting…' : (ws.note ? 'Retry' : 'Start');
    b.disabled = ws.running;
    b.onclick = (e) => { e.stopPropagation(); startSession(ws.id, b, null); };
    return b;
  }

  function statusTags(ws) {
    const el = document.createElement('div');
    el.className = 'tags';
    let any = false;
    if (ws.live > 0) {
      const p = document.createElement('span');
      p.className = 'pill live';
      p.innerHTML = '<i></i>' + ws.live + ' live';
      el.appendChild(p); any = true;
    } else if (ws.running) {
      const p = document.createElement('span');
      p.className = 'pill'; p.textContent = 'starting';
      el.appendChild(p); any = true;
    }
    const e = ws.lastEvent;
    if (e && !(ws.running && e.kind === 'started')) {
      const what = e.kind === 'exited' ? 'exited' + (e.cause ? ' ' + e.cause : '')
        : e.kind === 'attention' ? 'needs you' : e.kind;
      const s = document.createElement('span');
      s.className = e.kind === 'attention' ? 'pill warn' : 'why';
      s.textContent = what + ' ' + fmtWhen(e.at);
      el.appendChild(s); any = true;
    }
    return any ? el : null;
  }

  function topSessionRow(ws) {
    const top = ws.topSession;
    if (!top) return null;
    const when = [top.where, top.startedAt ? fmtWhen(top.startedAt) : ''].filter(Boolean).join(' · ');
    const body = '<i class="sdot"></i><span class="tmain"><span class="tname">' +
      esc(shortSessionName(top.name, ws.id)) + '</span>' +
      '<span class="when">' + esc(when) + '</span></span>';

    if (!top.url || !safeClaudeUrl(top.url)) {
      const el = document.createElement('div');
      el.className = 'top';
      el.innerHTML = body + '<span class="go small">local only</span>';
      return el;
    }
    const el = document.createElement('a');
    el.className = 'top';
    el.href = top.url; el.target = '_blank'; el.rel = 'noopener noreferrer';
    el.innerHTML = body + '<span class="go">↗</span>';
    const mode = openMode(ws.id);
    el.onclick = (e) => {
      e.stopPropagation();
      if (mode === 'browser') { e.preventDefault(); window.open(top.url, '_blank', 'noopener'); }
      if (mode === 'chrome') { e.preventDefault(); location.href = chromeUrl(top.url); }
    };
    return el;
  }

  // Inside one repo's card the leading workspace name is noise — the card
  // already says which repo this is, so the branch and time get the width.
  function shortSessionName(name, id) {
    const full = String(name || '');
    const prefix = id + ' · ';
    return full.indexOf(prefix) === 0 ? full.slice(prefix.length) : full;
  }

  function sessionsHint(ws) {
    const more = (ws.live || 0) - (ws.topSession ? 1 : 0);
    if (more > 0) return more + ' more session' + (more > 1 ? 's' : '');
    return ws.running ? 'sessions, options' : 'sessions, account, options';
  }

  function usageText(ws) {
    if (!ws.usage || !ws.usage.week) return '';
    const sum = u => (u.input || 0) + (u.output || 0) + (u.cacheRead || 0) + (u.cacheWrite || 0);
    const week = sum(ws.usage.week);
    if (!week) return '';
    return fmtTokens(sum(ws.usage.today)) + ' today · ' + fmtTokens(week) + ' this week';
  }

  function introLine(workspaces) {
    if (workspaces.some(w => (w.live || 0) > 0)) return null;
    const el = document.createElement('div');
    el.className = 'intro';
    el.textContent = 'Tap Start to run a Claude Code session on that machine. ' +
      'It works there with your files and databases; you drive it from here.';
    return el;
  }

  function shortPath(p) {
    const parts = String(p || '').split('/').filter(Boolean);
    return parts.length > 2 ? '…/' + parts.slice(-2).join('/') : p;
  }

  /* ---- the bottom sheet: every secondary control, one thumb away ---- */

  let sheetWs = null;

  function initSheet() {
    const sheet = document.getElementById('sheet');
    const close = (e) => { e.stopPropagation(); closeSheet(); };
    document.getElementById('scrim').onclick = close;
    document.getElementById('grab').onclick = close;
    addEventListener('keydown', (e) => { if (e.key === 'Escape') closeSheet(); });
    sheet.addEventListener('click', (e) => { if (e.target === sheet) closeSheet(); });
  }

  function closeSheet() {
    const sheet = document.getElementById('sheet');
    if (sheet.hidden) return;
    sheet.classList.remove('on');
    sheetWs = null;
    document.body.classList.remove('locked');
    setTimeout(() => { sheet.hidden = true; }, 240);
  }

  function openSheet(ws) {
    sheetWs = ws.id;
    const sheet = document.getElementById('sheet');
    document.getElementById('sheettitle').textContent = ws.id;
    document.getElementById('sheetsub').textContent = ws.path || '';
    const body = document.getElementById('sheetbody');
    body.innerHTML = '';

    // What you came for first: start it, or open what is already running.
    if (ws.sessionUrl && safeClaudeUrl(ws.sessionUrl)) {
      const open = primaryAction(ws);
      open.classList.add('block');
      open.textContent = 'Open the running session ↗';
      body.appendChild(open);
    } else if (!ws.running) {
      const box = startOptions(ws);
      if (box) body.appendChild(box);
      const b = document.createElement('button');
      b.className = 'btn primary block';
      b.textContent = ws.note ? 'Try starting again' : 'Start a session';
      b.onclick = (e) => { e.stopPropagation(); startSession(ws.id, b, box); };
      body.appendChild(b);
    }

    body.appendChild(sheetRow('Open links in', openMode(ws.id), () => {
      const order = ['app', 'browser', 'chrome'];
      setOpenMode(ws.id, order[(order.indexOf(openMode(ws.id)) + 1) % order.length]);
      render(lastWorkspaces);
      openSheet(lastWorkspaces.find(w => w.id === ws.id) || ws);
    }, 'app deep-links into the Claude app, browser stays here, chrome forces Chrome'));

    const isHidden = hidden().has(ws.id);
    body.appendChild(sheetRow(isHidden ? 'Unhide this repo' : 'Hide from this browser', '', () => {
      toggleHidden(ws.id);
      closeSheet();
    }, 'Nothing on the machine changes — this browser only'));

    if (ws.running) {
      const stop = sheetRow('Stop the session', '', (row) => stopSession(ws.id, row), '');
      stop.classList.add('bad');
      body.appendChild(stop);
    }

    const sessions = document.createElement('div');
    body.appendChild(sessions);
    loadSessions(ws, sessions);

    sheet.hidden = false;
    document.body.classList.add('locked');
    requestAnimationFrame(() => {
      sheet.classList.add('on');
      const panel = sheet.querySelector('.panel');
      panel.scrollTop = 0;
      panel.focus({ preventScroll: true });
    });
  }

  function sheetRow(label, value, onTap, desc) {
    const b = document.createElement('button');
    b.className = 'srow';
    const main = document.createElement('span');
    main.innerHTML = esc(label) + (desc ? '<span class="desc">' + esc(desc) + '</span>' : '');
    b.appendChild(main);
    if (value) {
      const v = document.createElement('span');
      v.className = 'sval'; v.textContent = value;
      b.appendChild(v);
    }
    const go = document.createElement('span');
    go.className = 'go'; go.textContent = '›';
    if (!value) go.style.marginLeft = 'auto';
    b.appendChild(go);
    b.onclick = (e) => { e.stopPropagation(); onTap(b); };
    return b;
  }

  // The start options are built even when the sheet is closed again: Start
  // reads them, so a profile chosen before tapping still applies.
  function startOptions(ws) {
    if (ws.running) return null;
    const box = document.createElement('div');
    box.className = 'startbox';
    if ((ws.profiles || []).length) {
      const field = document.createElement('div');
      field.className = 'field';
      field.innerHTML = '<label for="prof-' + esc(ws.id) + '">Claude account</label>';
      const sel = document.createElement('select');
      sel.id = 'prof-' + ws.id;
      sel.dataset.role = 'profile';
      const none = document.createElement('option');
      none.value = ''; none.textContent = 'default account';
      sel.appendChild(none);
      for (const p of ws.profiles) {
        const o = document.createElement('option');
        o.value = p; o.textContent = p;
        sel.appendChild(o);
      }
      field.appendChild(sel);
      box.appendChild(field);
    }
    const field = document.createElement('div');
    field.className = 'field';
    field.innerHTML = '<label for="name-' + esc(ws.id) + '">Session name</label>';
    const name = document.createElement('input');
    name.id = 'name-' + ws.id;
    name.type = 'text';
    name.placeholder = 'what is it for? (optional)';
    name.dataset.role = 'name';
    name.maxLength = 60;
    field.appendChild(name);
    box.appendChild(field);
    return box;
  }

  async function loadSessions(ws, box) {
    box.innerHTML = '<div class="grp"><span>sessions</span><i></i></div><div class="none">Loading…</div>';
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
      const tag = o.bridge ? '<span class="tag">bridge</span>' : '';
      const text = o.label ? esc(o.label) : esc(url.split('/').pop().slice(0, 14)) + '…';
      const dot = o.past ? '' : '<i class="sdot"></i>';
      el.innerHTML = dot + '<span class="smain"><span class="sname">' + text + tag + '</span>' +
        '<span class="when">' + esc(o.when || '') + '</span></span>' +
        '<span class="go">' + (o.past ? '↻' : '↗') + '</span>';
      box.appendChild(el);
    };
    const renderLocal = (label, when) => {
      const el = document.createElement('div');
      el.className = 's';
      el.innerHTML = '<i class="sdot"></i><span class="smain"><span class="sname">' + esc(label) + '</span>' +
        '<span class="when">' + esc(when) + ' · local only</span></span>';
      box.appendChild(el);
    };
    const note = (text) => {
      const el = document.createElement('div');
      el.className = 'hint'; el.textContent = text;
      box.appendChild(el);
    };
    try {
      const r = await fetch('/launch/sessions?workspace=' + encodeURIComponent(ws.id), { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      if (sheetWs !== ws.id) return; // the sheet moved on while this was in flight
      box.innerHTML = '';

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
      for (const h of (j.history || [])) if (h) addOlder(h.url, fmtWhen(h.at));
      for (const url of (ws.sessionLinks || []).slice().reverse()) addOlder(url, '');

      let localOnly = 0;
      const showB = showBridges();
      const liveCount = running.length + (showB ? bridges.filter(u => !running.some(s => s.url === u)).length : 0);
      if (liveCount) group('live now');
      for (const sess of running) {
        const where = whereLabel(sess);
        const when = where + ' · ' + fmtWhen(sess.startedAt);
        if (sess.url && safeClaudeUrl(sess.url) && !seen.has(sess.url)) {
          seen.add(sess.url);
          renderLink(sess.url, { label: shortSessionName(sess.name, ws.id), when: when });
          continue;
        }
        localOnly++;
        renderLocal(shortSessionName(sess.name, ws.id) || 'session', when);
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
      if (localOnly) note('local only = running on the laptop with no web link yet; type /remote-control in that session to reach it from here.');
      if (bridgeRows) note('bridge = started by hand on the laptop; its page shows only what you send from it.');
      if (bridgeHidden) note(bridgeHidden + ' bridge session' + (bridgeHidden > 1 ? 's' : '') + ' hidden — enable in Settings.');

      if (older.length) {
        group('earlier · not running');
        for (const row of older) renderLink(row.url, { past: true, when: row.when });
      }
      if (!liveCount && !older.length && !evs.length) {
        group('sessions');
        const none = document.createElement('div');
        none.className = 'none'; none.textContent = 'No sessions yet for this workspace.';
        box.appendChild(none);
        return;
      }
      if (evs.length) group('activity');
      for (const ev of evs) {
        const el = document.createElement('div');
        el.className = 'evrow';
        const what = ev.kind + (ev.cause ? ' · ' + ev.cause : '') + (ev.reason ? ' — ' + ev.reason : '');
        el.innerHTML = '<b>' + esc(what.slice(0, 90)) + '</b><span>' + esc(fmtWhen(ev.at)) + '</span>';
        box.appendChild(el);
      }
    } catch (e) {
      if (sheetWs !== ws.id) return;
      box.innerHTML = '';
      group('sessions');
      note('✗ ' + e.message);
    }
  }

  function whereLabel(sess) {
    const ep = String(sess.entrypoint || '');
    if (ep.indexOf('vscode') >= 0) return 'vscode';
    if (ep === 'sdk-cli') return 'remote';
    return sess.kind === 'interactive' ? 'terminal' : (sess.kind || 'session');
  }

  function fmtTokens(n) {
    if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B';
    if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n >= 1e3) return Math.round(n / 1e3) + 'k';
    return String(n || 0);
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

  async function stopSession(id, btn) {
    btn.disabled = true;
    btn.querySelector('span').textContent = 'Stopping…';
    try {
      const r = await fetch('/launch/stop', { method: 'POST', headers: auth,
        body: JSON.stringify({ workspace: id }) });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      closeSheet();
      toast('Stopping ' + id + '…');
      setTimeout(load, 1200);
    } catch (e) {
      btn.disabled = false;
      btn.querySelector('span').textContent = 'Stop the session';
      toast(e.message, true);
    }
  }

  async function startSession(id, btn, box) {
    const label = btn.textContent;
    btn.disabled = true; btn.textContent = 'Starting…';
    const pick = (role) => {
      const el = box && box.querySelector('[data-role="' + role + '"]');
      return el ? el.value.trim() : '';
    };
    try {
      const r = await fetch('/launch/start', { method: 'POST', headers: auth,
        body: JSON.stringify({ workspace: id, profile: pick('profile'), name: pick('name') }) });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      closeSheet();
      toast('Starting ' + id + '…');
      // Show it as starting straight away: the list is the same surface the
      // Start button lives on, and a card still offering Start invites a
      // second one before the daemon has answered the first.
      const w = lastWorkspaces.find(x => x.id === id);
      if (w) { w.running = true; w.note = ''; render(lastWorkspaces); }
      poll(id, 0);
    } catch (e) {
      btn.disabled = false; btn.textContent = label;
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
