package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"

	"github.com/spf13/cobra"
)

// Pairing exists so a phone never has to be handed the server's own bearer
// token. That token reaches corgi_exec and corgi_db_query, so a QR containing
// it is a credential for the machine — visible to anyone who sees the screen,
// and impossible to revoke for one device without re-pairing every other.

// pairRequest is what a client posts to /pair.
type pairRequest struct {
	Code   string `json:"code"`
	Device string `json:"device"`
}

type pairResponse struct {
	Token   string `json:"token"`
	Daemon  string `json:"daemon"`
	Device  string `json:"device"`
	Version string `json:"version"`
}

// maxPairBodyBytes bounds the request body. The payload is two short strings;
// anything larger is a mistake or an attempt to make the server allocate.
const maxPairBodyBytes = 4 << 10

// pairingHandler serves /pair while a pairing window is open: GET renders the
// scan-to-pair page, POST performs the pairing.
func pairingHandler(session *pairing.Session, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The POST response carries a device token and the GET page carries the
		// copy-paste connector: keep both out of any intermediary cache, and
		// stop content sniffing. Defense in depth — the default cloudflared
		// tunnel caches neither, but a corporate proxy on the client path might.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if r.Method == http.MethodGet {
			// The page holds no secret: the code travels only in the URL
			// fragment (which never reaches the server) and is typed back by
			// the page's own JS. Rendering it while closed would only invite a
			// form that cannot succeed.
			w.Header().Set(headerContentType, "text/html; charset=utf-8")
			if !session.Open() {
				// A person, not a client, lands here: a reopened QR link, an
				// expired one, or a phone that already paired. Say so in a page
				// that sends an already-paired browser on to the launcher.
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, pairClosedHTML)
				return
			}
			_, _ = fmt.Fprint(w, pairPageHTML)
			return
		}

		w.Header().Set(headerContentType, mimeJSON)

		if r.Method != http.MethodPost {
			writePairError(w, http.StatusMethodNotAllowed, "POST a {code, device} body to pair")
			return
		}
		if !session.Open() {
			// Deliberately vague about why: an expired window and a used one
			// are the same answer to anyone who should not be here.
			writePairError(w, http.StatusForbidden, "pairing is not open — run `corgi mcp --http --pair` on the machine")
			return
		}

		var req pairRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPairBodyBytes)).Decode(&req); err != nil {
			writePairError(w, http.StatusBadRequest, "could not read the pairing request")
			return
		}

		token, err := pairing.Pair(storePath, session, req.Code, req.Device)
		if err != nil {
			// Only errors about the caller's own input go back verbatim.
			// Anything else — a permission problem, a corrupt store — would
			// leak absolute paths and file modes to an unauthenticated, possibly
			// tunnelled caller, so it is logged locally and reported plainly.
			if errors.Is(err, pairing.ErrBadRequest) {
				writePairError(w, http.StatusForbidden, err.Error())
				return
			}
			utils.Infof("pairing failed: %v\n", err)
			writePairError(w, http.StatusInternalServerError, "pairing failed on the machine — check its output")
			return
		}

		host, _ := os.Hostname()
		utils.Infof("paired device %q\n", strings.TrimSpace(req.Device))
		_ = json.NewEncoder(w).Encode(pairResponse{
			Token:   token,
			Daemon:  host,
			Device:  strings.TrimSpace(req.Device),
			Version: APP_VERSION,
		})
	})
}

// pairPageHTML is the scan-to-pair page: the QR printed by `corgi agent up`
// points here with the code in the URL fragment. Self-contained, no external
// assets, nothing server-rendered — the fragment stays in the browser.
// pairClosedHTML is what a browser sees on a pairing link whose window is no
// longer open. Deliberately vague about why (used vs expired), like the POST
// path; it only helps a browser that already holds a token find the launcher.
const pairClosedHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>corgi pairing</title>
<style>
  body{font-family:-apple-system,system-ui,sans-serif;background:#0f1115;color:#e8e8e8;
       display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
  main{max-width:22rem;width:100%;padding:2rem}
  h1{font-size:1.3rem;margin:0 0 .3rem}
  p{color:#9aa0a6;font-size:.9rem;margin:.2rem 0 1.2rem;line-height:1.5}
  code{background:#1a1d23;padding:.15rem .35rem;border-radius:.3rem}
  a.open{display:inline-block;background:#7ee787;color:#0f1115;text-decoration:none;
      padding:.7rem 1.1rem;border-radius:.6rem;font-weight:600}
</style>
<main>
  <h1>🐕 This pairing link is closed</h1>
  <p id="msg">Pairing links work once and expire after two minutes.</p>
  <p id="paired" hidden>This phone is already paired with this machine.</p>
  <a id="app" class="open" href="/app" hidden>Open the launcher</a>
  <p id="fresh">Not paired yet? On the laptop run <code>corgi agent up --fresh</code> and scan the new QR.</p>
</main>
<script>
  let token = '';
  try { token = localStorage.getItem('corgi_token') || ''; } catch {}
  if (token) {
    document.getElementById('paired').hidden = false;
    document.getElementById('app').hidden = false;
    document.getElementById('fresh').hidden = true;
  }
</script>
`

const pairPageHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pair with corgi</title>
<style>
  body{font-family:-apple-system,system-ui,sans-serif;background:#0f1115;color:#e8e8e8;
       display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
  main{max-width:22rem;width:100%;padding:2rem}
  h1{font-size:1.3rem;margin:0 0 .3rem}
  p{color:#9aa0a6;font-size:.9rem;margin:.2rem 0 1.2rem}
  input,button{width:100%;box-sizing:border-box;font-size:1rem;padding:.7rem .8rem;border-radius:.6rem}
  input{border:1px solid #333;background:#1a1d23;color:#e8e8e8;margin-bottom:.8rem}
  button{border:0;background:#e8e8e8;color:#0f1115;font-weight:600;cursor:pointer}
  button:disabled{opacity:.5}
  #out{margin-top:1rem;font-size:.85rem}
  .ok{color:#7ee787}.err{color:#ff7b72;word-break:break-word}
  code{background:#1a1d23;padding:.15rem .35rem;border-radius:.3rem}
  pre{background:#1a1d23;border:1px solid #333;border-radius:.5rem;padding:.8rem;
      overflow-x:auto;font-size:.78rem;line-height:1.4;white-space:pre;margin:.6rem 0}
  #copy{margin-bottom:.4rem}
  a.open{display:inline-block;background:#7ee787;color:#0f1115;text-decoration:none;
      padding:.7rem 1.1rem;border-radius:.6rem;font-weight:600}
</style>
<main>
  <h1>🐕 Pair with corgi</h1>
  <p>Name this device, tap pair. The code came along in the QR you scanned.</p>
  <input id="device" placeholder="my-phone" autocomplete="off" autocapitalize="none">
  <button id="go">Pair</button>
  <div id="out"></div>
</main>
<script>
  const code = location.hash.slice(1);
  const out = document.getElementById('out');
  const btn = document.getElementById('go');
  const esc = s => String(s).replace(/[&<>"']/g, c =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  if (!code) { out.innerHTML = '<span class="err">No code in the link — rescan the QR from the terminal.</span>'; btn.disabled = true; }
  btn.onclick = async () => {
    const device = document.getElementById('device').value.trim() || 'my-phone';
    btn.disabled = true; btn.textContent = 'Pairing…';
    try {
      const r = await fetch('/pair', {method:'POST', headers:{'Content-Type':'application/json', 'ngrok-skip-browser-warning': '1'},
        body: JSON.stringify({code, device})});
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.status);
      try { localStorage.setItem('corgi_token', j.token); } catch (_) {}
      const mcpUrl = location.origin + '/mcp';
      const connector = JSON.stringify({mcpServers:{corgi:{url:mcpUrl,
        headers:{Authorization:'Bearer ' + j.token}}}}, null, 2);
      out.innerHTML =
        '<span class="ok">✓ Paired as <b>' + esc(device) + '</b> with <b>' +
          esc(j.daemon||'this machine') + '</b></span>' +
        '<p>Open the launcher to see your repos and start a session — one tap, ' +
          'no setup. Save it to your home screen to come back:</p>' +
        '<a class="open" href="/app">Open launcher →</a>' +
        '<p style="margin-top:1.4rem">Prefer the Claude app instead? Add corgi as a ' +
          'custom connector (on claude.ai) — tap to copy:</p>' +
        '<pre id="cfg">' + esc(connector) + '</pre>' +
        '<button id="copy">Copy connector config</button>' +
        '<p>This token is shown once; the launcher remembers it on this browser, ' +
          'and the config above is the only other copy.</p>';
      btn.remove();
      document.getElementById('copy').onclick = async (e) => {
        try { await navigator.clipboard.writeText(connector); e.target.textContent = '✓ Copied'; }
        catch { const r = document.createRange(); r.selectNode(document.getElementById('cfg'));
          getSelection().removeAllRanges(); getSelection().addRange(r); e.target.textContent = 'Selected — long-press to copy'; }
      };
    } catch (e) {
      out.innerHTML = '<span class="err">✗ ' + esc(e.message) + '</span>';
      btn.disabled = false; btn.textContent = 'Pair';
    }
  };
</script>
`

func writePairError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// announcePairing prints the code and how to use it. The code is short-lived
// and single-use, which is the whole reason it is safe to display.
func announcePairing(code, addr string) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  pairing code: %s\n", code)
	fmt.Fprintf(os.Stderr, "  valid for %s, single use\n", pairing.CodeTTL)
	fmt.Fprintf(os.Stderr, "  POST http://%s/pair  {\"code\":\"%s\",\"device\":\"my-phone\"}\n", localURL(addr), code)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Paired devices get their own token, revocable with `corgi mcp devices revoke <name>`.")
	fmt.Fprintln(os.Stderr, "")
}

// ---------------------------------------------------------------- devices CLI

var mcpDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List and revoke devices paired with corgi mcp",
	Run:   runMCPDevicesList,
}

var mcpDevicesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List paired devices",
	Run:   runMCPDevicesList,
}

var mcpDevicesRevokeCmd = &cobra.Command{
	Use:   "revoke <name>",
	Short: "Revoke one device's access, leaving the others working",
	Args:  cobra.ExactArgs(1),
	Run:   runMCPDevicesRevoke,
}

func mcpDeviceStorePath() string {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	return pairing.StorePath(dir)
}

func runMCPDevicesList(_ *cobra.Command, _ []string) {
	store, err := pairing.Load(mcpDeviceStorePath())
	if err != nil {
		exitWithError("mcp_devices_read", err, 1)
	}

	if utils.JSONOutput {
		// Hashes are omitted: they are not needed to answer "which devices are
		// paired", and printing them invites treating them as identifiers.
		type row struct {
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"createdAt"`
		}
		rows := make([]row, 0, len(store.Devices))
		for _, d := range store.Devices {
			rows = append(rows, row{Name: d.Name, CreatedAt: d.CreatedAt})
		}
		utils.PrintJSON(rows)
		return
	}

	if len(store.Devices) == 0 {
		fmt.Println("No paired devices. Run `corgi mcp --http :8765 --pair` to pair one.")
		return
	}
	for _, d := range store.Devices {
		fmt.Printf("%-24s paired %s\n", d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
}

func runMCPDevicesRevoke(_ *cobra.Command, args []string) {
	path := mcpDeviceStorePath()
	store, err := pairing.Load(path)
	if err != nil {
		exitWithError("mcp_devices_read", err, 1)
	}
	if !store.Revoke(args[0]) {
		exitWithError("mcp_device_unknown", fmt.Errorf("no paired device called %q", args[0]), 1)
	}
	if err := pairing.Save(path, store); err != nil {
		exitWithError("mcp_devices_write", err, 1)
	}
	utils.Infof("revoked %s — other devices are unaffected\n", args[0])
}

func init() {
	mcpDevicesCmd.AddCommand(mcpDevicesListCmd, mcpDevicesRevokeCmd)
	mcpCmd.AddCommand(mcpDevicesCmd)
}
