package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils/agent/daemon"
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

	running := map[string]wsRunState{}
	if dir, derr := agentDir(); derr == nil {
		if st, _ := daemon.ReadStatus(dir); st != nil {
			for _, ws := range st.Workspaces {
				running[ws.WorkspaceID] = wsRunState{running: ws.Running, url: ws.SessionURL}
			}
		}
	}

	out := make([]launchWorkspace, 0, len(registry.Workspaces))
	for _, ws := range registry.Sorted() {
		s := running[ws.ID]
		out = append(out, launchWorkspace{
			ID: ws.ID, Aliases: ws.Aliases, Path: ws.AbsPath,
			Status: string(ws.Status), Running: s.running, SessionURL: s.url,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeLaunchJSON(w, map[string]any{"workspaces": out})
}

type wsRunState struct {
	running bool
	url     string
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

func launcherPageHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, launcherPageHTML)
}

func setLaunchHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeLaunchJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", mimeJSON)
	_ = json.NewEncoder(w).Encode(v)
}

func writeLaunchError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", mimeJSON)
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
      display:flex;align-items:center;justify-content:space-between;gap:.6rem}
  .ws .name{font-weight:600}
  .ws .path{color:#8a90a6;font-size:.72rem;word-break:break-all;margin-top:.15rem}
  .dot{width:.55rem;height:.55rem;border-radius:50%;background:#3a3f4b;flex:0 0 auto}
  .dot.on{background:#7ee787}
  button{border:0;border-radius:.55rem;padding:.5rem .9rem;font-size:.85rem;font-weight:600;cursor:pointer;
      background:#e8e8e8;color:#0f1115}
  button:disabled{opacity:.5}
  a.open{display:inline-block;background:#7ee787;color:#0f1115;text-decoration:none;
      padding:.5rem .9rem;border-radius:.55rem;font-weight:600;font-size:.85rem}
  .msg{color:#9aa0a6;font-size:.9rem;margin:1rem 0}
  .err{color:#ff7b72}
  code{background:#1a1d23;padding:.1rem .3rem;border-radius:.3rem}
</style>
<header>🐕 corgi<small id="host">your machine</small></header>
<main><div id="list" class="msg">Loading…</div></main>
<script>
  const esc = s => String(s).replace(/[&<>"']/g, c =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const token = (() => { try { return localStorage.getItem('corgi_token') || ''; } catch { return ''; } })();
  const list = document.getElementById('list');
  const auth = { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' };

  if (!token) {
    list.className = 'msg';
    list.innerHTML = 'Not paired on this browser yet. On your laptop run ' +
      '<code>corgi agent up</code> and scan the QR to pair, then come back here.';
  } else {
    load();
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
      left.innerHTML = '<div class="name"><span class="dot' + (ws.running ? ' on' : '') + '"></span> ' +
        esc(ws.id) + '</div><div class="path">' + esc(ws.path) + '</div>';
      const right = document.createElement('div');
      if (ws.sessionUrl) {
        const a = document.createElement('a');
        a.className = 'open'; a.href = ws.sessionUrl; a.textContent = 'Open session'; a.target = '_blank';
        right.appendChild(a);
      } else {
        const b = document.createElement('button');
        b.textContent = ws.running ? 'Starting…' : 'Start';
        b.disabled = ws.running;
        b.onclick = () => startSession(ws.id, b);
        right.appendChild(b);
      }
      row.appendChild(left); row.appendChild(right);
      list.appendChild(row);
    }
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
      if (ws && ws.sessionUrl) { render(j.workspaces); return; }
    } catch {}
    setTimeout(() => poll(id, n + 1), 1000);
  }
</script>
`
