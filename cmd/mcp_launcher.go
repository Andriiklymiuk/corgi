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
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// The launcher is corgi's own phone UI: after pairing, the same browser page
// lists the machine's workspaces and starts a session in one tap, then hands
// back the claude.ai link. It needs no claude.ai custom connector — the page
// holds the device token and calls these endpoints directly:
//
//   GET  /app                 the launcher page (static; uses the stored token)
//   GET  /launch/workspaces   list workspaces with running state + sessionUrl
//   POST /launch/start        {workspace, profile?} → start a session
//
// /launch/* sit behind the same bearer/device-token auth as /mcp, and start is
// the same capability as corgi_session_start — no new power, just a browser
// transport. /app is static and carries no secret.

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

	out := make([]launchWorkspace, 0, len(registry.Workspaces))
	for _, ws := range registry.Sorted() {
		s := running[ws.ID]
		out = append(out, launchWorkspace{
			ID: ws.ID, Aliases: ws.Aliases, Path: ws.AbsPath,
			Status: string(ws.Status), Running: s.running, SessionURL: s.url,
			SessionLinks: s.sessions, Note: s.note,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
	result, err := mcpSessionStart(req.Workspace, req.Profile)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, result)
}

// claudeSession is one entry from `claude agents --json`.
type claudeSession struct {
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"`
	StartedAt int64  `json:"startedAt"`
	PID       int    `json:"pid"`
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
	})
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
  .ws{background:var(--card);border:1px solid var(--line);border-radius:1rem;padding:1rem 1.05rem;margin:.7rem 0;
      display:flex;align-items:center;justify-content:space-between;gap:.7rem;flex-wrap:wrap;
      box-shadow:0 1px 3px rgba(0,0,0,.3)}
  .ws .name{font-weight:650;display:flex;align-items:center;gap:.5rem;font-size:.98rem}
  .ws .path{color:var(--dim2);font-size:.7rem;word-break:break-all;margin-top:.2rem;
      font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
  .ws .wnote{color:var(--red);font-size:.72rem;margin-top:.35rem;line-height:1.45}
  .dot{width:.55rem;height:.55rem;border-radius:50%;background:#3a4152;flex:0 0 auto}
  .dot.on{background:var(--green);box-shadow:0 0 0 3px rgba(126,231,135,.14),0 0 8px rgba(126,231,135,.45)}
  .sbtn{background:none;border:0;color:var(--dim);font-size:.72rem;font-weight:650;cursor:pointer;
      padding:.25rem 0 0;margin-top:.3rem}
  .sessions{flex-basis:100%;margin-top:.6rem;border-top:1px solid var(--line);padding-top:.55rem}
  .sessions .s{display:flex;align-items:center;justify-content:space-between;gap:.6rem;font-size:.76rem;
      color:#c9cfda;padding:.34rem .1rem;text-decoration:none}
  a.s span:first-child{color:var(--green)}
  .sessions .s .when{color:var(--dim2);font-size:.68rem;flex:0 0 auto}
  .sessions .none,.sessions .hint{color:var(--dim2);font-size:.7rem;line-height:1.45;padding:.2rem 0}
  .tag{font-size:.6rem;font-weight:700;color:var(--amber);border:1px solid rgba(255,166,87,.4);
      border-radius:.35rem;padding:.05rem .3rem;margin-left:.45rem;vertical-align:1px}
  button{border:0;border-radius:.65rem;padding:.55rem 1rem;font-size:.85rem;font-weight:650;cursor:pointer;
      background:#232a39;color:var(--text);transition:transform .05s}
  button:active{transform:scale(.97)}
  button:disabled{opacity:.5}
  a.open,button.open{display:inline-block;background:var(--green);color:#08110a;text-decoration:none;border:0;
      padding:.55rem 1rem;border-radius:.65rem;font-weight:700;font-size:.85rem;cursor:pointer}
  .right{display:flex;flex-direction:column;align-items:flex-end;gap:.45rem}
  .modes{display:flex;font-size:.68rem;color:var(--dim);align-items:center;background:var(--card2);
      border:1px solid var(--line);border-radius:.55rem;padding:.14rem}
  .modes span{padding:0 .3rem 0 .45rem;font-size:.64rem}
  .modes .m{background:none;border:1px solid transparent;color:var(--dim);border-radius:.42rem;
      padding:.2rem .5rem;font-size:.68rem;font-weight:650}
  .modes .m.sel{background:#2b3345;color:var(--text)}
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
    <p>Each workspace above has an <b>open in</b> switch — <b>app</b> deep-links into the Claude app; <b>browser</b> keeps the session here in this browser; <b>chrome</b> forces Chrome via its URL scheme (needs Chrome installed) — right for a workspace on a different Claude account than the app is signed into. Remembered per workspace, on this browser only.</p>
    <label class="toggle"><input type="checkbox" id="showbridges"> Show hand-started (bridge) sessions</label>
    <p>A <b>bridge</b> is a remote-control session someone started on the laptop itself. Its claude.ai page shows only what you send from it — until then it looks empty. The full transcript stays on the laptop.</p>
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
  const auth = { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' };
  // Set in JS (not a static href) so the page source carries no external link;
  // this is a user navigation to the Claude session list, not a loaded asset.
  try { document.getElementById('allsessions').href = 'https://claude.ai/code'; } catch {}

  let lastWorkspaces = [];
  const openMode = id => { try { return localStorage.getItem('corgi_open_' + id) || 'app'; } catch { return 'app'; } };
  const setOpenMode = (id, m) => { try { localStorage.setItem('corgi_open_' + id, m); } catch {} };
  const showBridges = () => { try { return localStorage.getItem('corgi_show_bridges') !== '0'; } catch { return true; } };
  const setShowBridges = on => { try { localStorage.setItem('corgi_show_bridges', on ? '1' : '0'); } catch {} };

  if (!token) {
    list.className = 'msg';
    list.innerHTML = 'Not paired on this browser yet. On your laptop run ' +
      '<code>corgi agent up</code> and scan the QR to pair, then come back here.';
  } else {
    initSettings();
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

  function render(workspaces) {
    lastWorkspaces = workspaces;
    if (!workspaces.length) {
      list.className = 'msg';
      list.innerHTML = 'No workspaces registered. On the laptop: <code>corgi agent scan ~/dev</code>.';
      return;
    }
    list.className = '';
    list.innerHTML = '';
    for (const ws of workspaces) {
      const row = document.createElement('div');
      row.className = 'ws';
      const left = document.createElement('div');
      // A note is shown only when the workspace is NOT running (a live session's
      // stale lastReason is not a problem to flag).
      const note = (!ws.running && ws.note) ? '<div class="wnote">' + esc(ws.note) + '</div>' : '';
      left.innerHTML = '<div class="name"><span class="dot' + (ws.running ? ' on' : '') + '"></span> ' +
        esc(ws.id) + '</div><div class="path">' + esc(ws.path) + '</div>' + note;
      const sbtn = document.createElement('button');
      sbtn.className = 'sbtn'; sbtn.textContent = 'sessions ⌄';
      const sessionsBox = document.createElement('div');
      sessionsBox.className = 'sessions'; sessionsBox.style.display = 'none';
      sbtn.onclick = () => toggleSessions(ws, sbtn, sessionsBox);
      left.appendChild(sbtn);
      const right = document.createElement('div');
      right.className = 'right';
      if (ws.sessionUrl && safeClaudeUrl(ws.sessionUrl)) {
        right.appendChild(openControl(ws));
        right.appendChild(modeSwitch(ws.id));
      } else {
        const b = document.createElement('button');
        b.textContent = ws.running ? 'Starting…' : (ws.note ? 'Retry' : 'Start');
        b.disabled = ws.running;
        b.onclick = () => startSession(ws.id, b);
        right.appendChild(b);
      }
      row.appendChild(left); row.appendChild(right); row.appendChild(sessionsBox);
      list.appendChild(row);
    }
  }

  async function toggleSessions(ws, btn, box) {
    if (box.style.display !== 'none') { box.style.display = 'none'; btn.textContent = 'sessions \u2304'; return; }
    box.style.display = ''; btn.textContent = 'sessions \u2303';
    box.innerHTML = '';
    // Openable links come ONLY from the per-session URLs remote control printed
    // (captured by the daemon). Ids from the claude CLI are local UUIDs the
    // site does not resolve; those rows render below as plain status, no link.
    const renderLink = (url, isBridge) => {
      const el = document.createElement('a');
      el.className = 's';
      el.href = url; el.target = '_blank'; el.rel = 'noopener noreferrer';
      const m = openMode(ws.id);
      if (m === 'browser') el.onclick = (e) => { e.preventDefault(); window.open(url, '_blank', 'noopener'); };
      if (m === 'chrome') el.onclick = (e) => { e.preventDefault(); location.href = chromeUrl(url); };
      const tag = isBridge ? '<span class="tag" title="Hand-started on the laptop \u2014 its web page may look empty">bridge</span>' : '';
      el.innerHTML = '<span>' + esc(url.split('/').pop().slice(0, 18)) + '\u2026' + tag + '</span><span class="when">open \u2197</span>';
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
      const s = j.sessions || [];
      if (!s.length && !shown.size && !bridges.length) { info.textContent = 'No sessions yet for this workspace.'; return; }
      info.remove();
      for (const sess of s) {
        const el = document.createElement('div');
        el.className = 's';
        el.innerHTML = '<span>' + esc(sess.name || 'session') + '</span>' +
          '<span class="when">' + esc(sess.kind || '') + ' \u00b7 ' + esc(fmtWhen(sess.startedAt)) + '</span>';
        box.appendChild(el);
      }
    } catch (e) { info.textContent = '\u2717 ' + e.message; }
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
      b.className = 'open'; b.textContent = 'Open session';
      b.onclick = () => {
        if (mode === 'chrome') { location.href = chromeUrl(ws.sessionUrl); }
        else { window.open(ws.sessionUrl, '_blank', 'noopener'); }
      };
      return b;
    }
    const a = document.createElement('a');
    a.className = 'open'; a.href = ws.sessionUrl; a.textContent = 'Open session';
    a.target = '_blank'; a.rel = 'noopener noreferrer';
    return a;
  }

  function modeSwitch(id) {
    const cur = openMode(id);
    const wrap = document.createElement('div');
    wrap.className = 'modes';
    const lbl = document.createElement('span');
    lbl.textContent = 'open in';
    wrap.appendChild(lbl);
    for (const m of ['app', 'browser', 'chrome']) {
      const b = document.createElement('button');
      b.className = 'm' + (cur === m ? ' sel' : '');
      b.textContent = m;
      b.onclick = () => { setOpenMode(id, m); render(lastWorkspaces); };
      wrap.appendChild(b);
    }
    return wrap;
  }

  async function startSession(id, btn) {
    btn.disabled = true; btn.textContent = 'Starting…';
    try {
      const r = await fetch('/launch/start', { method: 'POST', headers: auth,
        body: JSON.stringify({ workspace: id }) });
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
