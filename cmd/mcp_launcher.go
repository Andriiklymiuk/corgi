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
	SessionLinks []string         `json:"sessionLinks,omitempty"`
	Note         string           `json:"note,omitempty"`
	Live         int              `json:"live"`
	Usage        *usage.Report    `json:"usage,omitempty"`
	LastEvent    *launchLastEvent `json:"lastEvent,omitempty"`
	Profiles     []string         `json:"profiles,omitempty"`
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
		row.Live, row.LastEvent = workspaceActivity(ws.ID, ws.AbsPath)
		row.Usage = workspaceUsage(ws.ID, ws.AbsPath)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func workspaceActivity(id, absPath string) (int, *launchLastEvent) {
	// The pid-file reader, never listClaudeSessions: its fallback shells out,
	// and this runs per workspace on a list the phone polls while starting.
	var live int
	if _, configDir, ok := workspaceSessionTarget(id); ok && absPath != "" {
		if sessions, read := localClaudeSessions(absPath, configDir); read {
			live = len(sessions)
		}
	}
	dir, err := agentDir()
	if err != nil {
		return live, nil
	}
	recent := events.NewLog(dir).Read(id, 1)
	if len(recent) == 0 {
		return live, nil
	}
	e := recent[0]
	return live, &launchLastEvent{Kind: e.Kind, Cause: e.Cause, Reason: e.Reason, At: e.At.UnixMilli()}
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

func sessionHistory(workspaceID string) []launchHistoryEntry {
	dir, err := agentDir()
	if err != nil {
		return []launchHistoryEntry{}
	}
	seen := map[string]bool{}
	out := []launchHistoryEntry{}
	for _, e := range events.NewLog(dir).Read(workspaceID, 0) {
		if e.Kind != "session" || e.URL == "" || seen[e.URL] {
			continue
		}
		seen[e.URL] = true
		out = append(out, launchHistoryEntry{URL: e.URL, At: e.At.UnixMilli()})
		if len(out) >= 20 {
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
<meta name="theme-color" content="#0b0d12">
<title>corgi</title>
<style>
  :root{--bg:#0b0d12;--card:#161a23;--card2:#11151d;--line:#262c3a;--text:#e9ecf1;
      --dim:#8b93a7;--dim2:#6b7285;--green:#7ee787;--amber:#ffa657;--red:#ff7b72}
  *{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
  body{font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;
      padding-bottom:env(safe-area-inset-bottom);-webkit-font-smoothing:antialiased}
  header{padding:calc(1.2rem + env(safe-area-inset-top)) 1.2rem .3rem;max-width:34rem;margin:0 auto}
  .brand{display:flex;align-items:center;gap:.7rem}
  .logo{width:2.6rem;height:2.6rem;border-radius:.9rem;background:linear-gradient(135deg,#1e2634,#131824);
      border:1px solid var(--line);display:flex;align-items:center;justify-content:center;font-size:1.35rem;flex:0 0 auto}
  h1{font-size:1.25rem;margin:0;letter-spacing:.01em}
  header small{display:block;color:var(--dim);font-size:.78rem;font-weight:400;margin-top:.1rem}
  main{padding:.4rem 1.2rem 2.2rem;max-width:34rem;margin:0 auto}
  .ws{background:var(--card);border:1px solid var(--line);border-radius:.9rem;padding:.8rem .9rem;margin:.55rem 0;
      box-shadow:0 1px 3px rgba(0,0,0,.3)}
  .head{display:flex;align-items:center;gap:.7rem}
  .main{min-width:0;flex:1}
  .ws .name{font-weight:650;display:flex;align-items:center;gap:.5rem;font-size:.98rem;white-space:nowrap;
      overflow:hidden;text-overflow:ellipsis}
  .ws .path{color:var(--dim2);font-size:.7rem;margin-top:.15rem;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
      white-space:nowrap;overflow:hidden;text-overflow:ellipsis;cursor:pointer}
  .ws .path.full{white-space:normal;word-break:break-all}
  .ws .wnote{color:var(--red);font-size:.72rem;margin-top:.5rem;line-height:1.45}
  .dot{width:.55rem;height:.55rem;border-radius:50%;background:#3a4152;flex:0 0 auto}
  .dot.on{background:var(--green);box-shadow:0 0 0 3px rgba(126,231,135,.14),0 0 8px rgba(126,231,135,.45)}
  .meta{display:flex;align-items:center;gap:.4rem;flex-wrap:wrap;margin-top:.3rem;font-size:.72rem;color:var(--dim2)}
  .meta .live{color:var(--green)}
  .meta .why{color:var(--dim)}
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
  .chip{background:none;border:1px solid var(--line);color:var(--dim);font-size:.7rem;font-weight:650;
      padding:.3rem .65rem;border-radius:999px;cursor:pointer}
  .chip b{color:var(--text);font-weight:650}
  .chip.danger{border-color:rgba(255,123,114,.45);color:var(--red);margin-left:auto}
  .sessions{margin-top:.65rem;border-top:1px solid var(--line);padding-top:.5rem}
  .sessions .s{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.76rem;
      color:#c9cfda;padding:.34rem .1rem;text-decoration:none}
  a.s span:first-child{color:var(--green)}
  .sessions .s .when{color:var(--dim2);font-size:.68rem;flex:0 0 auto}
  .sessions .none,.sessions .hint{color:var(--dim2);font-size:.7rem;line-height:1.45;padding:.2rem 0}
  .tag{font-size:.6rem;font-weight:700;color:var(--amber);border:1px solid rgba(255,166,87,.4);
      border-radius:.35rem;padding:.05rem .3rem;margin-left:.45rem;vertical-align:1px}
  button{border:0;border-radius:.6rem;padding:.5rem .95rem;font-size:.84rem;font-weight:650;cursor:pointer;
      background:#232a39;color:var(--text);transition:transform .05s;flex:0 0 auto}
  button:active{transform:scale(.97)}
  button:disabled{opacity:.5}
  a.open,button.open{display:inline-block;background:var(--green);color:#08110a;text-decoration:none;border:0;
      padding:.5rem .95rem;border-radius:.6rem;font-weight:700;font-size:.84rem;cursor:pointer;flex:0 0 auto}
  .msg{color:var(--dim);font-size:.9rem;margin:1rem 0;line-height:1.5}
  .err{color:var(--red)}
  code{background:var(--card);border:1px solid var(--line);padding:.1rem .35rem;border-radius:.35rem;font-size:.85em}
  details.settings{margin:1.8rem 0 0;background:var(--card2);border:1px solid var(--line);
      border-radius:1rem;padding:.4rem 1rem}
  details.settings summary{color:var(--dim);font-size:.85rem;cursor:pointer;padding:.5rem 0;list-style:none}
  details.settings summary::-webkit-details-marker{display:none}
  details.settings p{color:var(--dim);font-size:.76rem;line-height:1.55}
  label.toggle{display:flex;align-items:center;gap:.5rem;color:var(--dim);font-size:.78rem;
      padding:.3rem 0 .7rem;cursor:pointer}
  label.toggle input{accent-color:var(--green);width:1rem;height:1rem;margin:0}
  pre{background:var(--bg);border:1px solid var(--line);border-radius:.6rem;padding:.7rem;
      overflow-x:auto;font-size:.7rem;line-height:1.45;white-space:pre;color:#c9cfda}
  .foot{text-align:center;margin-top:1.6rem;font-size:.85rem}
  .foot a{color:var(--green);text-decoration:none}
</style>
<header>
  <div class="brand"><span class="logo">🐕</span>
    <div><h1>corgi</h1><small id="host">your machine</small></div>
  </div>
</header>
<main>
  <div id="list" class="msg">Loading…</div>
  <details class="settings" id="settings" hidden>
    <summary>⚙ Settings</summary>
    <p><b>Claude app connector.</b> Prefer the Claude app? Add corgi as a custom connector on claude.ai (Settings → Connectors → Add custom), so the app can control this machine too. Tap to copy:</p>
    <pre id="cfg"></pre>
    <button id="copycfg">Copy connector config</button>
    <p id="copymsg" class="msg"></p>
    <p>Each workspace has an <b>open in</b> pill — tap it to cycle: <b>app</b> deep-links into the Claude app; <b>browser</b> keeps the session here; <b>chrome</b> forces Chrome via its URL scheme (needs Chrome installed) — right for a workspace on a different Claude account than the app is signed into. Remembered per workspace, on this browser only.</p>
    <p><b>Hiding workspaces.</b> Each card has a <b>hide</b> chip — useful when showing this screen to someone. Hidden cards collapse into one "N hidden — show" button, on this browser only; nothing on the machine changes.</p>
    <label class="toggle"><input type="checkbox" id="showbridges"> Show hand-started (bridge) sessions</label>
    <p>A <b>bridge</b> is a remote-control session someone started on the laptop itself. Its claude.ai page shows only what you send from it — until then it looks empty. The full transcript stays on the laptop.</p>
    <p><b>Paired devices.</b> Every device that scanned a pairing QR. Revoking one leaves the others working.</p>
    <div id="devices" class="msg">Loading…</div>
    <p><b>Something not starting?</b> The same checks as <code>corgi agent doctor</code>, from here.</p>
    <button id="rundoctor">Run doctor</button>
    <div id="doctor"></div>
    <p><b>Push notifications.</b> Set <code>notifyUrl</code> in the agent config on the laptop (for example an ntfy.sh topic you subscribe to in the ntfy app) and every session restart or failure reaches your phone. See <code>corgi agent doctor</code> and the agent docs.</p>
  </details>
  <p class="foot">
    <a id="allsessions" target="_blank" rel="noopener">See all your sessions on claude.ai ↗</a>
  </p>
</main>
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
  const hidden = () => { try { return new Set(JSON.parse(localStorage.getItem('corgi_hidden') || '[]')); } catch { return new Set(); } };
  const setHidden = s => { try { localStorage.setItem('corgi_hidden', JSON.stringify([...s])); } catch {} };
  const toggleHidden = id => { const h = hidden(); h.has(id) ? h.delete(id) : h.add(id); setHidden(h); render(lastWorkspaces); };
  let revealHidden = false;
  const setShowBridges = on => { try { localStorage.setItem('corgi_show_bridges', on ? '1' : '0'); } catch {} };

  if (!token) {
    list.className = 'msg';
    list.innerHTML = 'Not paired on this browser yet. On your laptop run ' +
      '<code>corgi agent up</code> and scan the QR to pair, then come back here.';
  } else {
    initSettings();
    loadInfo();
    load();
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
      el.textContent = bits.join(' · ');
      if (!j.daemon) el.style.color = 'var(--red)';
    } catch {}
  }

  function render(workspaces) {
    lastWorkspaces = workspaces;
    if (!workspaces.length) {
      list.className = 'msg';
      list.innerHTML = 'No workspaces registered. On the laptop: <code>corgi agent scan ~/dev</code>.';
      return;
    }
    list.className = '';
    list.innerHTML = '';
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
      main.innerHTML = '<div class="name"><span class="dot' + (ws.running ? ' on' : '') + '"></span>' + esc(ws.id) + '</div>';
      const path = document.createElement('div');
      path.className = 'path'; path.title = ws.path; path.textContent = shortPath(ws.path);
      path.onclick = () => {
        const full = path.classList.toggle('full');
        path.textContent = full ? ws.path : shortPath(ws.path);
      };
      main.appendChild(path);
      const meta = metaLine(ws);
      if (meta) main.appendChild(meta);
      head.appendChild(main);
      if (ws.sessionUrl && safeClaudeUrl(ws.sessionUrl)) {
        head.appendChild(openControl(ws));
      } else {
        const b = document.createElement('button');
        b.textContent = ws.running ? 'Starting…' : (ws.note ? 'Retry' : 'Start');
        b.disabled = ws.running;
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
      sbtn.className = 'chip'; sbtn.textContent = 'sessions ⌄';
      sbtn.onclick = () => toggleSessions(ws, sbtn, sessionsBox);
      actions.appendChild(sbtn);
      actions.appendChild(modeSwitch(ws.id));
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

  function metaLine(ws) {
    const bits = [];
    if (ws.live > 0) bits.push('<span class="live">' + ws.live + ' live</span>');
    if (ws.usage && ws.usage.week) {
      const t = ws.usage.today, w = ws.usage.week;
      const sum = u => (u.input || 0) + (u.output || 0) + (u.cacheRead || 0) + (u.cacheWrite || 0);
      bits.push('<span title="tokens today / this week">' + fmtTokens(sum(t)) + ' today · ' + fmtTokens(sum(w)) + ' this week</span>');
    }
    const e = ws.lastEvent;
    if (e && !(ws.running && e.kind === 'started')) {
      const what = e.kind === 'exited' ? 'exited' + (e.cause ? ' · ' + esc(e.cause) : '')
        : e.kind === 'attention' ? 'needs you'
        : esc(e.kind);
      bits.push('<span class="why">' + what + ' · ' + esc(fmtWhen(e.at)) + '</span>');
    }
    if (!bits.length) return null;
    const el = document.createElement('div');
    el.className = 'meta';
    el.innerHTML = bits.join('<span>·</span>');
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

  function shortPath(p) {
    const parts = String(p || '').split('/').filter(Boolean);
    return parts.length > 2 ? '…/' + parts.slice(-2).join('/') : p;
  }

  async function toggleSessions(ws, btn, box) {
    if (box.style.display !== 'none') { box.style.display = 'none'; btn.textContent = 'sessions \u2304'; return; }
    box.style.display = ''; btn.textContent = 'sessions \u2303';
    box.innerHTML = '';
    // Openable links come ONLY from the per-session URLs remote control printed
    // (captured by the daemon). Ids from the claude CLI are local UUIDs the
    // site does not resolve; those rows render below as plain status, no link.
    const renderLink = (url, isBridge, when, label) => {
      const el = document.createElement('a');
      el.className = 's';
      el.href = url; el.target = '_blank'; el.rel = 'noopener noreferrer';
      const m = openMode(ws.id);
      if (m === 'browser') el.onclick = (e) => { e.preventDefault(); window.open(url, '_blank', 'noopener'); };
      if (m === 'chrome') el.onclick = (e) => { e.preventDefault(); location.href = chromeUrl(url); };
      const tag = isBridge ? '<span class="tag" title="Hand-started on the laptop \u2014 its web page may look empty">bridge</span>' : '';
      const text = label ? esc(label) : esc(url.split('/').pop().slice(0, 18)) + '\u2026';
      el.innerHTML = '<span>' + text + tag + '</span><span class="when">' + esc(when || '') + 'open \u2197</span>';
      box.appendChild(el);
    };
    const note = (text) => {
      const el = document.createElement('div');
      el.className = 'hint'; el.textContent = text;
      box.appendChild(el);
    };
    // Captured from corgi-supervised output; the fetch below adds bridge
    // pointers from disk, which cover hand-started remote-control sessions.
    const shown = new Set();
    const links = (ws.sessionLinks || []).filter(safeClaudeUrl);
    for (const url of links.slice().reverse()) { shown.add(url); renderLink(url, false); }
    const info = document.createElement('div');
    info.className = 'none'; info.textContent = 'Loading\u2026';
    box.appendChild(info);
    try {
      const r = await fetch('/launch/sessions?workspace=' + encodeURIComponent(ws.id), { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      const bridges = (j.links || []).filter(safeClaudeUrl).filter(u => !shown.has(u));
      if (showBridges()) {
        for (const url of bridges) { shown.add(url); renderLink(url, true); }
        if (bridges.length) note('bridge = started by hand on the laptop; its page shows only what you send from it.');
      } else if (bridges.length) {
        note(bridges.length + ' bridge session' + (bridges.length > 1 ? 's' : '') + ' hidden \u2014 enable in Settings.');
      }
      const past = (j.history || []).filter(h => h && safeClaudeUrl(h.url) && !shown.has(h.url));
      for (const h of past) { shown.add(h.url); renderLink(h.url, false, fmtWhen(h.at) + ' \u00b7 '); }
      const evs = j.events || [];
      const s = j.sessions || [];
      if (!s.length && !shown.size && !bridges.length && !evs.length) { info.textContent = 'No sessions yet for this workspace.'; return; }
      info.remove();
      let localOnly = 0;
      for (const sess of s) {
        const where = whereLabel(sess);
        if (sess.url && safeClaudeUrl(sess.url) && !shown.has(sess.url)) {
          shown.add(sess.url);
          renderLink(sess.url, false, where + ' \u00b7 ' + fmtWhen(sess.startedAt) + ' \u00b7 ', sess.name);
          continue;
        }
        if (sess.url && shown.has(sess.url)) continue;
        localOnly++;
        const el = document.createElement('div');
        el.className = 's';
        el.innerHTML = '<span>' + esc(sess.name || 'session') + '</span>' +
          '<span class="when">' + esc(where) + ' \u00b7 ' + esc(fmtWhen(sess.startedAt)) + ' \u00b7 local only</span>';
        box.appendChild(el);
      }
      if (localOnly) note('local only = running on the laptop with no web link yet; type /remote-control in that session to reach it from here.');
      for (const ev of evs) {
        const el = document.createElement('div');
        el.className = 'evrow';
        const what = ev.kind + (ev.cause ? ' · ' + ev.cause : '') + (ev.reason ? ' — ' + ev.reason : '');
        el.innerHTML = '<b>' + esc(what.slice(0, 90)) + '</b><span>' + esc(fmtWhen(ev.at)) + '</span>';
        box.appendChild(el);
      }
    } catch (e) { info.textContent = '\u2717 ' + e.message; }
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

  // app mode uses a real anchor tap so iOS deep-links into the Claude app;
  // browser mode opens via JS, which keeps the session in this browser; chrome
  // mode forces Chrome via its URL scheme (right for a workspace signed into a
  // different Claude account than the app — e.g. work vs personal).
  function openControl(ws) {
    const mode = openMode(ws.id);
    if (mode === 'browser' || mode === 'chrome') {
      const b = document.createElement('button');
      b.className = 'open'; b.textContent = 'Open';
      b.onclick = () => {
        if (mode === 'chrome') { location.href = chromeUrl(ws.sessionUrl); }
        else { window.open(ws.sessionUrl, '_blank', 'noopener'); }
      };
      return b;
    }
    const a = document.createElement('a');
    a.className = 'open'; a.href = ws.sessionUrl; a.textContent = 'Open';
    a.target = '_blank'; a.rel = 'noopener noreferrer';
    return a;
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
        setTimeout(load, 1200);
      } catch (e) {
        b.disabled = false; b.textContent = 'Stop';
        const err = document.createElement('div');
        err.className = 'msg err'; err.textContent = '✗ ' + e.message;
        b.parentElement.appendChild(err);
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
      poll(id, 0);
    } catch (e) {
      btn.disabled = false; btn.textContent = 'Retry';
      const err = document.createElement('div');
      err.className = 'msg err'; err.textContent = '✗ ' + e.message;
      btn.parentElement.appendChild(err);
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
      if (ws && (ws.sessionUrl || (!ws.running && ws.note))) { render(j.workspaces); return; }
    } catch {}
    setTimeout(() => poll(id, n + 1), 1000);
  }
</script>
`
