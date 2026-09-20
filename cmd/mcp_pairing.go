package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"

	"github.com/spf13/cobra"
)

type pairRequest struct {
	Code   string `json:"code"`
	Device string `json:"device"`
	PubKey string `json:"pubKey,omitempty"`
}

type pairResponse struct {
	Token        string `json:"token"`
	Daemon       string `json:"daemon"`
	Device       string `json:"device"`
	Version      string `json:"version"`
	ServerPubKey string `json:"serverPubKey,omitempty"`
	Role         string `json:"role,omitempty"`
	PublicURL    string `json:"publicUrl,omitempty"`
}

const maxPairBodyBytes = 4 << 10

func pairingHandler(session *pairing.Session, storePath string) http.Handler {
	return pairingHandlerWithRole(session, storePath, "")
}

func pairingHandlerWithRole(session *pairing.Session, storePath, role string) http.Handler {
	w := &pairWindow{}
	w.set(session, role)
	return pairingHandlerFor(w, storePath)
}

type pairWindow struct {
	mu      sync.Mutex
	session *pairing.Session
	role    string
}

func (p *pairWindow) set(s *pairing.Session, role string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil && p.session != s {
		p.session.Close()
	}
	p.session, p.role = s, role
}

func (p *pairWindow) get() (*pairing.Session, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session, p.role
}

func pairingHandlerFor(window *pairWindow, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, role := window.get()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if r.Method == http.MethodGet {
			w.Header().Set(headerContentType, "text/html; charset=utf-8")
			if !session.Open() {
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
			writePairError(w, http.StatusForbidden, "pairing is not open — run `corgi mcp --http --pair` on the machine")
			return
		}

		var req pairRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPairBodyBytes)).Decode(&req); err != nil {
			writePairError(w, http.StatusBadRequest, "could not read the pairing request")
			return
		}

		serverPub := ""
		if strings.TrimSpace(req.PubKey) != "" {
			server, kerr := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(filepath.Dir(storePath)))
			if kerr != nil {
				utils.Infof("pairing: e2e key: %v\n", kerr)
				writePairError(w, http.StatusInternalServerError, "pairing failed on the machine — check its output")
				return
			}
			serverPub = pairing.PublicKeyString(server.PublicKey())
		}
		token, err := pairing.PairWithRole(storePath, session, req.Code, req.Device, req.PubKey, role)
		if err != nil {
			if errors.Is(err, pairing.ErrBadRequest) {
				writePairError(w, http.StatusForbidden, err.Error())
				return
			}
			utils.Infof("pairing failed: %v\n", err)
			writePairError(w, http.StatusInternalServerError, "pairing failed on the machine — check its output")
			return
		}

		host, _ := os.Hostname()
		if role != "" {
			utils.Infof("paired device %q as %s\n", strings.TrimSpace(req.Device), role)
		} else {
			utils.Infof("paired device %q\n", strings.TrimSpace(req.Device))
		}
		_ = json.NewEncoder(w).Encode(pairResponse{
			Token:        token,
			Daemon:       host,
			Device:       strings.TrimSpace(req.Device),
			Version:      APP_VERSION,
			ServerPubKey: serverPub,
			Role:         role,
			PublicURL:    strings.TrimSuffix(launcherURL(), "/app"),
		})
	})
}

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
  <p id="msg">Pairing links work once and expire after ten minutes.</p>
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
    location.replace('/app');
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
      btn.remove();
      out.innerHTML =
        '<span class="ok">✓ Paired as <b>' + esc(device) + '</b> with <b>' +
          esc(j.daemon||'this machine') + '</b></span>' +
        '<p>Opening your repos… <a class="open" href="/app">tap here</a> if nothing happens.</p>';
      location.replace('/app');
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

const pairLaunchName = "pair.launch"

type pairLaunch struct {
	Code string `json:"code"`
	TTL  string `json:"ttl,omitempty"`
}

func writePairLaunch(dir, code string, ttl time.Duration) error {
	if _, err := pairing.NewSessionWithCode(code, ttl); err != nil {
		return err
	}
	data, _ := json.Marshal(pairLaunch{Code: pairing.NormalizeCode(code), TTL: ttl.String()})
	return os.WriteFile(filepath.Join(dir, pairLaunchName), data, 0o600)
}

func takePairLaunch(dir string) (*pairing.Session, bool) {
	path := filepath.Join(dir, pairLaunchName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	_ = os.Remove(path)
	var l pairLaunch
	if json.Unmarshal(raw, &l) != nil {
		return nil, false
	}
	ttl, _ := time.ParseDuration(l.TTL)
	session, err := pairing.NewSessionWithCode(l.Code, ttl)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pairing: ignoring the launch code:", err)
		return nil, false
	}
	return session, true
}

func announcePairing(session *pairing.Session, addr string) {
	code := session.Code()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  pairing code: %s\n", code)
	fmt.Fprintf(os.Stderr, "  valid for %s, single use\n", time.Until(session.ExpiresAt()).Round(time.Minute))
	fmt.Fprintf(os.Stderr, "  POST http://%s/pair  {\"code\":\"%s\",\"device\":\"my-phone\"}\n", localURL(addr), code)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Paired devices get their own token, revocable with `corgi mcp devices revoke <name>`.")
	fmt.Fprintln(os.Stderr, "")
}

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
		type row struct {
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"createdAt"`
			Encrypted bool      `json:"encrypted"`
			Role      string    `json:"role,omitempty"`
			ExpiresAt time.Time `json:"expiresAt,omitempty"`
		}
		rows := make([]row, 0, len(store.Devices))
		for _, d := range store.Devices {
			rows = append(rows, row{Name: d.Name, CreatedAt: d.CreatedAt, Encrypted: d.Encrypted(), Role: d.Role, ExpiresAt: d.ExpiresAt})
		}
		utils.PrintJSON(rows)
		return
	}

	if len(store.Devices) == 0 {
		fmt.Println("No paired devices. Run `corgi mcp --http :8765 --pair` to pair one.")
		return
	}
	for _, d := range store.Devices {
		how := "plain — pair again from the app for end-to-end encryption"
		if d.Encrypted() {
			how = "end-to-end encrypted"
		}
		if d.Viewer() {
			how += " · reads only"
		}
		if !d.ExpiresAt.IsZero() {
			how = "oauth access token"
			if d.Expired(time.Now()) {
				how += " · expired"
			} else {
				how += " · expires " + d.ExpiresAt.Local().Format("2006-01-02 15:04")
			}
		}
		fmt.Printf("%-24s paired %s · %s\n", d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"), how)
	}
}

func runMCPDevicesRevoke(_ *cobra.Command, args []string) {
	path := mcpDeviceStorePath()
	store, err := pairing.Load(path)
	if err != nil {
		exitWithError("mcp_devices_read", err, 1)
	}
	device, _ := store.Find(args[0])
	if !store.Revoke(args[0]) {
		exitWithError("mcp_device_unknown", fmt.Errorf("no paired device called %q", args[0]), 1)
	}
	if err := pairing.Save(path, store); err != nil {
		exitWithError("mcp_devices_write", err, 1)
	}
	revokeOAuthFamily(filepath.Dir(path), device.Family)
	utils.Infof("revoked %s — other devices are unaffected\n", args[0])
}

func init() {
	mcpDevicesCmd.AddCommand(mcpDevicesListCmd, mcpDevicesRevokeCmd)
	mcpCmd.AddCommand(mcpDevicesCmd)
}

const (
	pairRequestName = "pair.request"
	pairAnswerName  = "pair.json"
)

type pairAnswer struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	Role      string    `json:"role,omitempty"`
	PublicURL string    `json:"publicUrl,omitempty"`
	LocalURL  string    `json:"localUrl,omitempty"`
	Daemon    string    `json:"daemon"`
}

func watchPairRequests(dir string, window *pairWindow) {
	req := filepath.Join(dir, pairRequestName)
	for {
		time.Sleep(time.Second)
		raw, err := os.ReadFile(req)
		if err != nil {
			continue
		}
		_ = os.Remove(req)
		role := strings.TrimSpace(string(raw))
		if role != pairing.RoleViewer && role != pairing.RolePeer {
			role = ""
		}
		session, code, err := pairing.NewSession()
		if err != nil {
			continue
		}
		window.set(session, role)
		host, _ := os.Hostname()
		ans := pairAnswer{Code: code, ExpiresAt: time.Now().Add(pairing.CodeTTL), Role: role, PublicURL: strings.TrimSuffix(launcherURL(), "/app"), LocalURL: strings.TrimSuffix(localLauncherURL(), "/app"), Daemon: host}
		data, _ := json.Marshal(ans)
		_ = os.WriteFile(filepath.Join(dir, pairAnswerName), data, 0o600)
		utils.Infof("pairing: a new window opened for %s\n", firstNonEmpty(role, "a phone"))
	}
}
