package cmd

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

const (
	maxPendingAuths = 20
	pendingAuthTTL  = 10 * time.Minute
	// approveCodeAlphabet is pairing's code alphabet without 0 and 1, so
	// pairing.NormalizeCode keeps every character a page shows.
	approveCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// newApproveCode is the short code a consent page shows for
// `corgi agent approve ABCD-2345`; unique among pending authorizations.
func (oa *oauthServer) newApproveCodeLocked() (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		out := make([]byte, 8)
		for i, v := range b {
			out[i] = approveCodeAlphabet[int(v)%len(approveCodeAlphabet)]
		}
		code := string(out[:4]) + "-" + string(out[4:])
		if _, taken := oa.pendingByCodeLocked(code); !taken {
			return code, nil
		}
	}
	return "", errors.New("could not pick an unused approval code")
}

func (oa *oauthServer) pendingByCodeLocked(code string) (*pendingAuth, bool) {
	code = pairing.NormalizeCode(code)
	for _, p := range oa.pending {
		if pairing.NormalizeCode(p.approveCode) == code {
			return p, true
		}
	}
	return nil, false
}

// resolveClient finds the client an authorize request names: a metadata
// document when client_id is an https URL, else the DCR store.
func (oa *oauthServer) resolveClient(r *http.Request, clientID string) (oauthClient, error) {
	if isCIMDClientID(clientID) {
		return oa.cimd.client(r.Context(), clientID, oa.hosts)
	}
	oa.mu.Lock()
	c, ok := oa.state.findClient(clientID)
	oa.mu.Unlock()
	if !ok {
		return oauthClient{}, errors.New("unknown client_id; register first or use a client metadata URL")
	}
	return c, nil
}

// authorizeHandler is GET /oauth/authorize. The client and redirect URI are
// checked before anything else and a failure there is a 400 page — never a
// redirect, or the endpoint would bounce browsers to any URL asked.
func (oa *oauthServer) authorizeHandler(w http.ResponseWriter, r *http.Request) {
	oa.mu.Lock()
	if oa.limitedLocked(w) {
		oa.mu.Unlock()
		return
	}
	oa.sweepLocked()
	oa.mu.Unlock()

	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		oa.consentError(w, "client_id and redirect_uri are required")
		return
	}
	client, err := oa.resolveClient(r, clientID)
	if err != nil {
		oa.consentError(w, "corgi does not know this client: "+err.Error())
		return
	}
	registered := ""
	for _, u := range client.RedirectURIs {
		if sameRedirectURI(u, redirectURI) {
			registered = u
			break
		}
	}
	if registered == "" || !oa.hosts.allowedRedirectURI(redirectURI) {
		oa.consentError(w, "the redirect URI is not one this client registered")
		return
	}

	// From here the redirect target is trusted and errors travel back on it.
	state := q.Get("state")
	if q.Get("response_type") != "code" {
		oa.redirectError(w, r, redirectURI, state, "unsupported_response_type", "only response_type=code")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		oa.redirectError(w, r, redirectURI, state, "invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}

	oa.mu.Lock()
	if len(oa.pending) >= maxPendingAuths {
		oa.mu.Unlock()
		oa.redirectError(w, r, redirectURI, state, "temporarily_unavailable", "too many sign-ins waiting; approve or let one expire")
		return
	}
	id, err := randomToken("")
	if err != nil {
		oa.mu.Unlock()
		oa.consentError(w, "could not start the sign-in")
		return
	}
	approveCode, err := oa.newApproveCodeLocked()
	if err != nil {
		oa.mu.Unlock()
		oa.consentError(w, "could not start the sign-in")
		return
	}
	p := &pendingAuth{
		id: id, client: client, redirectURI: redirectURI, codeChallenge: challenge,
		state: state, approveCode: approveCode, expires: oa.now().Add(pendingAuthTTL),
	}
	oa.pending[id] = p
	oa.mu.Unlock()

	target, _ := url.Parse(redirectURI)
	setConsentHeaders(w)
	_ = consentPage.Execute(w, consentView{
		ClientName:  client.Name,
		Host:        target.Hostname(),
		Loopback:    isLoopbackHost(target.Hostname()),
		PendingID:   id,
		ApproveCode: approveCode,
	})
}

// setConsentHeaders: the consent page must never render inside another
// site's frame — a browser holding corgi_token could be clickjacked into
// Approve. Inline script and same-origin fetch only.
func setConsentHeaders(w http.ResponseWriter) {
	setLaunchHeaders(w)
	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// consentError is the 400 page for a request whose redirect target cannot
// be trusted.
func (oa *oauthServer) consentError(w http.ResponseWriter, msg string) {
	setConsentHeaders(w)
	w.WriteHeader(http.StatusBadRequest)
	_ = consentErrorPage.Execute(w, msg)
}

// redirectError sends an OAuth error back to a trusted redirect URI.
func (oa *oauthServer) redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", oa.issuerURL())
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// approveRequest is what the consent page (id) or `corgi agent approve`
// (code) posts.
type approveRequest struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// approveHandler is POST /oauth/approve with a device token: the resource
// owner is present. Mints the authorization code and answers with the
// redirect the browser should follow.
func (oa *oauthServer) approveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	d, ok := authorizedDeviceFull(oa.deviceStore, r.Header.Get("Authorization"))
	if !ok || d.Viewer() || d.Family != "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="corgi"`)
		oauthError(w, http.StatusUnauthorized, "unauthorized", "approving needs a paired device's token; a viewer or an oauth token cannot approve")
		return
	}
	var req approveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxOAuthBody)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "body must be JSON with id or code")
		return
	}
	oa.mu.Lock()
	defer oa.mu.Unlock()
	oa.sweepLocked()
	var p *pendingAuth
	if req.ID != "" {
		p = oa.pending[req.ID]
	} else if req.Code != "" {
		p, _ = oa.pendingByCodeLocked(req.Code)
	}
	if p == nil {
		oauthError(w, http.StatusNotFound, "not_found", "no sign-in is waiting under that id or code; it may have expired")
		return
	}
	redirect, err := oa.approveLocked(p)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	// The page that posted the id follows the redirect itself and the
	// pending entry is done. A CLI approval leaves it for the page to poll.
	if req.ID != "" {
		delete(oa.pending, p.id)
	}
	w.Header().Set(headerContentType, mimeJSON)
	w.Header().Set("Cache-Control", "no-store")
	body, _ := json.Marshal(map[string]string{"status": "approved", "client": p.client.Name, "redirect": redirect})
	_, _ = w.Write(body)
}

// approveLocked mints the code once and remembers the redirect on the
// pending entry, so a page polling after a CLI approval finds it.
func (oa *oauthServer) approveLocked(p *pendingAuth) (string, error) {
	if p.redirect != "" {
		return p.redirect, nil
	}
	code, err := oa.issueCodeLocked(p.client, p.redirectURI, p.codeChallenge)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(p.redirectURI)
	q := u.Query()
	q.Set("code", code)
	if p.state != "" {
		q.Set("state", p.state)
	}
	q.Set("iss", oa.issuerURL())
	u.RawQuery = q.Encode()
	p.redirect = u.String()
	return p.redirect, nil
}

// pendingHandler is GET /oauth/pending/<id>: the consent page polls it after
// showing the approval code. Approved once → the entry is consumed.
func (oa *oauthServer) pendingHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, oauthPendingPath)
	oa.mu.Lock()
	defer oa.mu.Unlock()
	oa.sweepLocked()
	w.Header().Set(headerContentType, mimeJSON)
	w.Header().Set("Cache-Control", "no-store")
	p, ok := oa.pending[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"gone"}`))
		return
	}
	if p.redirect == "" {
		_, _ = w.Write([]byte(`{"status":"pending"}`))
		return
	}
	delete(oa.pending, id)
	body, _ := json.Marshal(map[string]string{"status": "approved", "redirect": p.redirect})
	_, _ = w.Write(body)
}

type consentView struct {
	ClientName  string
	Host        string
	Loopback    bool
	PendingID   string
	ApproveCode string
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="color-scheme" content="dark"><meta name="robots" content="noindex">
<title>Connect {{.ClientName}} to corgi</title>
<style>
:root{--bg:#08090a;--card:#101113;--line:#212327;--text:#eceef1;--dim:#8a8f98;--ok:#4cc38a;--warn:#f5a524}
*{box-sizing:border-box}body{margin:0;font-family:-apple-system,system-ui,sans-serif;background:var(--bg);color:var(--text);
display:flex;min-height:100vh;align-items:center;justify-content:center;padding:1.5rem}
.card{background:var(--card);border:1px solid var(--line);border-radius:1rem;padding:1.6rem;max-width:26rem;width:100%}
h1{font-size:1.15rem;margin:0 0 .6rem}p{color:var(--dim);line-height:1.45;margin:.4rem 0}
.host{color:var(--text);font-family:ui-monospace,Menlo,monospace}
.warn{color:var(--warn)}button{width:100%;margin-top:1rem;padding:.8rem;border:0;border-radius:.7rem;background:var(--ok);color:#06110b;font-weight:600;font-size:1rem;cursor:pointer}
button:disabled{opacity:.5}code{display:block;margin:.8rem 0;padding:.7rem;background:#000;border-radius:.6rem;font-size:1.15rem;letter-spacing:.08em;text-align:center;font-family:ui-monospace,Menlo,monospace}
.cmd{font-size:.9rem;letter-spacing:0;text-align:left;user-select:all}.hidden{display:none}#err{color:#f16a6a}
</style></head><body><div class="card">
<h1>Connect <span id="client">{{.ClientName}}</span> to corgi</h1>
<p>It will be sent back to <span class="host">{{.Host}}</span>{{if .Loopback}}<span class="warn"> — a program on your computer will receive the code</span>{{end}}.</p>
<p>A connected client can read your stack, start and steer sessions, and run what your workspaces allow. Destructive tools still ask in Claude.</p>
<div id="same" class="hidden"><button id="approve">Approve</button></div>
<div id="other" class="hidden">
<p>Prove you are at the machine. In a terminal there, run:</p>
<code class="cmd">corgi agent approve {{.ApproveCode}}</code>
<p>or open <span class="host">corgi agent dashboard</span> in this browser first, then reload this page.</p>
<p id="wait">Waiting for approval…</p>
</div>
<p id="err"></p>
</div>
<script>
(function(){
  var id={{.PendingID}};
  var tok='';try{tok=localStorage.getItem('corgi_token')||''}catch(e){}
  var same=document.getElementById('same'),other=document.getElementById('other'),err=document.getElementById('err');
  function fail(m){err.textContent=m}
  function go(r){location.replace(r.redirect)}
  if(tok){
    same.classList.remove('hidden');
    var b=document.getElementById('approve');
    b.onclick=function(){b.disabled=true;
      fetch('/oauth/approve',{method:'POST',headers:{'Authorization':'Bearer '+tok,'Content-Type':'application/json'},body:JSON.stringify({id:id})})
      .then(function(r){return r.json().then(function(j){if(!r.ok)throw new Error(j.error_description||j.error||r.status);return j})})
      .then(go).catch(function(e){b.disabled=false;fail(String(e.message||e));if(/401|token/.test(String(e.message)))(same.classList.add('hidden'),other.classList.remove('hidden'),poll())});
    };
  } else { other.classList.remove('hidden'); poll(); }
  function poll(){
    fetch('/oauth/pending/'+encodeURIComponent(id),{cache:'no-store'}).then(function(r){return r.json()}).then(function(j){
      if(j.status==='approved')return go(j);
      if(j.status==='gone'){fail('This sign-in expired. Go back to Claude and connect again.');return}
      setTimeout(poll,2000);
    }).catch(function(){setTimeout(poll,4000)});
  }
})();
</script></body></html>`))

var consentErrorPage = template.Must(template.New("consentError").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark"><title>corgi — cannot sign in</title>
<style>body{margin:0;font-family:-apple-system,system-ui,sans-serif;background:#08090a;color:#eceef1;display:flex;min-height:100vh;align-items:center;justify-content:center;padding:1.5rem}
.card{background:#101113;border:1px solid #212327;border-radius:1rem;padding:1.6rem;max-width:26rem}h1{font-size:1.1rem;margin:0 0 .6rem}p{color:#8a8f98;line-height:1.45}</style></head>
<body><div class="card"><h1>corgi cannot start this sign-in</h1><p>{{.}}</p><p>Nothing was sent anywhere. Close this tab and try again from the client.</p></div></body></html>`))

// approvePendingLocally is what `corgi agent approve` does: it talks to the
// MCP server on this machine with a freshly minted local device token.
func approvePendingLocally(agentDir, code string) (string, error) {
	addr, err := readLocalMCPAddr(agentDir)
	if err != nil {
		return "", err
	}
	store := pairing.StorePath(agentDir)
	const name = "corgi agent approve"
	token, err := pairing.PairLocal(store, name)
	if err != nil {
		return "", err
	}
	defer func() {
		if s, err := pairing.Load(store); err == nil && s.Revoke(name) {
			_ = pairing.Save(store, s)
		}
	}()
	body, _ := json.Marshal(approveRequest{Code: code})
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+oauthApprovePath, strings.NewReader(string(body))) // NOSONAR — loopback on this machine
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", bearerPrefix+token)
	req.Header.Set(headerContentType, mimeJSON)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("the MCP server at %s did not answer: %w", addr, err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxOAuthBody)).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		if out["error_description"] != "" {
			return "", errors.New(out["error_description"])
		}
		return "", fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return out["client"], nil
}
