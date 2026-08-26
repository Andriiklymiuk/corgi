package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	Note       string   `json:"note,omitempty"`
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
	registry.Reconcile(dirHasComposeFile)

	var status *daemon.Status
	if dir, derr := agentDir(); derr == nil {
		status, _ = daemon.ReadStatus(dir)
	}
	out := buildLaunchWorkspaces(registry, status)
	writeLaunchJSON(w, map[string]any{"workspaces": out})
}

type wsRunState struct {
	running bool
	url     string
	note    string
}

// buildLaunchWorkspaces joins the registry with the daemon's live status into
// the launcher rows. A start the daemon refused (sensitive, unknown profile,
// unreachable, bad bin) leaves a diagnostic warning, not a run state — merging
// it in is what stops the phone showing "Starting…" then silently giving up.
func buildLaunchWorkspaces(registry *workspace.Registry, status *daemon.Status) []launchWorkspace {
	running := map[string]wsRunState{}
	if status != nil {
		for _, ws := range status.Workspaces {
			running[ws.WorkspaceID] = wsRunState{running: ws.Running, url: ws.SessionURL, note: ws.LastReason}
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
			Status: string(ws.Status), Running: s.running, SessionURL: s.url, Note: s.note,
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
	writeLaunchJSON(w, map[string]any{"sessions": listClaudeSessions(absPath, configDir)})
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
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>corgi</title>
<style>
  body{font-family:-apple-system,system-ui,sans-serif;background:#0f1115;color:#e8e8e8;margin:0}
  header{padding:1.4rem 1.2rem .6rem;font-size:1.3rem;font-weight:600}
  header small{display:block;color:#9aa0a6;font-size:.8rem;font-weight:400;margin-top:.2rem}
  main{padding:0 1.2rem 2rem;max-width:34rem;margin:0 auto}
  .ws{background:#1a1d23;border:1px solid #2a2e37;border-radius:.7rem;padding:.9rem 1rem;margin:.6rem 0;
      display:flex;align-items:center;justify-content:space-between;gap:.6rem;flex-wrap:wrap}
  .ws .name{font-weight:600}
  .sbtn{background:none;border:0;color:#8a90a6;font-size:.72rem;font-weight:600;cursor:pointer;
      padding:.2rem 0;margin-top:.25rem}
  .sessions{flex-basis:100%;margin-top:.5rem;border-top:1px solid #2a2e37;padding-top:.5rem}
  .sessions .s{display:flex;justify-content:space-between;gap:.6rem;font-size:.76rem;color:#c8cdd6;padding:.22rem 0}
  .sessions .s .when{color:#8a90a6;font-size:.7rem;flex:0 0 auto}
  .sessions .none{color:#8a90a6;font-size:.74rem}
  .ws .path{color:#8a90a6;font-size:.72rem;word-break:break-all;margin-top:.15rem}
  .ws .wnote{color:#ff7b72;font-size:.72rem;margin-top:.3rem;line-height:1.4}
  .dot{width:.55rem;height:.55rem;border-radius:50%;background:#3a3f4b;flex:0 0 auto}
  .dot.on{background:#7ee787}
  button{border:0;border-radius:.55rem;padding:.5rem .9rem;font-size:.85rem;font-weight:600;cursor:pointer;
      background:#e8e8e8;color:#0f1115}
  button:disabled{opacity:.5}
  a.open,button.open{display:inline-block;background:#7ee787;color:#0f1115;text-decoration:none;border:0;
      padding:.5rem .9rem;border-radius:.55rem;font-weight:600;font-size:.85rem;cursor:pointer}
  .right{display:flex;flex-direction:column;align-items:flex-end;gap:.35rem}
  .modes{display:flex;gap:.3rem;font-size:.68rem;color:#8a90a6;align-items:center}
  .modes span{margin-right:.1rem}
  .modes .m{background:#12151b;border:1px solid #2a2e37;color:#9aa0a6;border-radius:.4rem;
      padding:.12rem .4rem;font-size:.68rem;font-weight:600}
  .modes .m.sel{background:#2a2e37;color:#e8e8e8;border-color:#3a3f4b}
  .msg{color:#9aa0a6;font-size:.9rem;margin:1rem 0}
  .err{color:#ff7b72}
  code{background:#1a1d23;padding:.1rem .3rem;border-radius:.3rem}
  details.settings{margin:1.6rem 0 0;border-top:1px solid #2a2e37;padding-top:1rem}
  details.settings summary{color:#9aa0a6;font-size:.85rem;cursor:pointer}
  details.settings p{color:#8a90a6;font-size:.78rem;line-height:1.5}
  pre{background:#1a1d23;border:1px solid #2a2e37;border-radius:.5rem;padding:.7rem;
      overflow-x:auto;font-size:.72rem;line-height:1.4;white-space:pre;color:#c8cdd6}
</style>
<header>🐕 corgi<small id="host">your machine</small></header>
<main>
  <div id="list" class="msg">Loading…</div>
  <details class="settings" id="settings" hidden>
    <summary>⚙ Settings</summary>
    <p><b>Claude app connector.</b> Prefer the Claude app? Add corgi as a custom connector on claude.ai (Settings → Connectors → Add custom), so the app can control this machine too. Tap to copy:</p>
    <pre id="cfg"></pre>
    <button id="copycfg">Copy connector config</button>
    <p id="copymsg" class="msg"></p>
    <p>Each workspace above has an <b>open in</b> switch — <b>app</b> deep-links into the Claude app; <b>browser</b> keeps the session here in this browser (use it for a workspace on a different Claude account than the app is signed into). Remembered per workspace, on this browser only.</p>
  </details>
  <p class="msg" style="text-align:center;margin-top:1.4rem">
    <a id="allsessions" target="_blank" rel="noopener" style="color:#7ee787;text-decoration:none">See all your sessions on claude.ai ↗</a>
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
      sbtn.onclick = () => toggleSessions(ws.id, sbtn, sessionsBox);
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

  async function toggleSessions(id, btn, box) {
    if (box.style.display !== 'none') { box.style.display = 'none'; btn.textContent = 'sessions ⌄'; return; }
    box.style.display = ''; btn.textContent = 'sessions ⌃';
    box.innerHTML = '<div class="none">Loading…</div>';
    try {
      const r = await fetch('/launch/sessions?workspace=' + encodeURIComponent(id), { headers: auth });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      const s = j.sessions || [];
      if (!s.length) { box.innerHTML = '<div class="none">No active sessions. corgi lists these from the claude CLI — none are running for this workspace.</div>'; return; }
      box.innerHTML = '';
      for (const sess of s) {
        const el = document.createElement('div');
        el.className = 's';
        el.innerHTML = '<span>' + esc(sess.name || sess.sessionId || 'session') +
          ' <span class="when">· ' + esc(sess.kind || '') + '</span></span>' +
          '<span class="when">' + esc(fmtWhen(sess.startedAt)) + '</span>';
        box.appendChild(el);
      }
    } catch (e) { box.innerHTML = '<div class="none">✗ ' + esc(e.message) + '</div>'; }
  }

  function fmtWhen(ms) {
    if (!ms) return '';
    const diff = (Date.now() - ms) / 1000;
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    return new Date(ms).toLocaleDateString();
  }

  // app mode uses a real anchor tap so iOS deep-links into the Claude app;
  // browser mode opens via JS, which keeps the session in this browser (right
  // for a workspace signed into a different Claude account than the app).
  function openControl(ws) {
    if (openMode(ws.id) === 'browser') {
      const b = document.createElement('button');
      b.className = 'open'; b.textContent = 'Open session';
      b.onclick = () => window.open(ws.sessionUrl, '_blank', 'noopener');
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
    wrap.appendChild(document.createTextNode('open in:'));
    for (const m of ['app', 'browser']) {
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
