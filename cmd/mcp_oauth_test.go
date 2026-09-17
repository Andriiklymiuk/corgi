package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

// newTestOAuthServer is an OAuth server on a loopback issuer with a device
// store holding one full device (token returned) in a temp agent dir.
func newTestOAuthServer(t *testing.T) (*oauthServer, *http.ServeMux, string) {
	t.Helper()
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	dir := tempAgentHome(t)
	store := pairing.StorePath(dir)
	deviceToken, err := pairing.PairLocal(store, "laptop browser")
	if err != nil {
		t.Fatal(err)
	}
	origins := newOriginAllowlist("127.0.0.1:18765", nil)
	oa := newOAuthServer("127.0.0.1:18765", newOAuthClientHosts(nil, nil), origins, store, oauthStatePath(dir))
	mux := http.NewServeMux()
	oa.mount(mux)
	mux.Handle("/mcp", mcpEndpointGuard(origins, bearerAuthWithOAuth("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), store, oa)))
	return oa, mux, deviceToken
}

func getJSON(t *testing.T, h http.Handler, path string, hdr map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec, body
}

func TestOAuthMetadataDocuments(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	for _, path := range []string{oauthPRMPath, oauthPRMPath + "/mcp"} {
		rec, body := getJSON(t, mux, path, map[string]string{"Host": "evil.example"})
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "max-age=300" {
			t.Fatalf("%s: %d %v", path, rec.Code, rec.Header())
		}
		if body["resource"] != "http://127.0.0.1:18765/mcp" {
			t.Errorf("%s resource = %v (issuer must never come from Host)", path, body["resource"])
		}
		if as, _ := body["authorization_servers"].([]any); len(as) != 1 || as[0] != "http://127.0.0.1:18765" {
			t.Errorf("%s authorization_servers = %v", path, body["authorization_servers"])
		}
		if body["resource_name"] != "corgi" || body["resource_documentation"] != oauthDocsURL {
			t.Errorf("%s = %v", path, body)
		}
	}

	oa.setIssuer("https://corgi.example.ngrok.app/")
	for _, path := range []string{oauthASMetadataPath, oauthASMetadataPath + "/mcp"} {
		rec, body := getJSON(t, mux, path, nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		want := map[string]string{
			"issuer":                 "https://corgi.example.ngrok.app",
			"authorization_endpoint": "https://corgi.example.ngrok.app/oauth/authorize",
			"token_endpoint":         "https://corgi.example.ngrok.app/oauth/token",
			"registration_endpoint":  "https://corgi.example.ngrok.app/oauth/register",
			"revocation_endpoint":    "https://corgi.example.ngrok.app/oauth/revoke",
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("%s %s = %v, want %s", path, k, body[k], v)
			}
		}
		if body["client_id_metadata_document_supported"] != true {
			t.Errorf("%s: CIMD flag missing", path)
		}
		if m, _ := body["token_endpoint_auth_methods_supported"].([]any); len(m) != 1 || m[0] != "none" {
			t.Errorf("%s token auth methods = %v", path, body["token_endpoint_auth_methods_supported"])
		}
		if m, _ := body["code_challenge_methods_supported"].([]any); len(m) != 1 || m[0] != "S256" {
			t.Errorf("%s pkce methods = %v", path, body["code_challenge_methods_supported"])
		}
		if _, has := body["scopes_supported"]; has {
			t.Errorf("%s must not list scopes", path)
		}
	}

	req := httptest.NewRequest(http.MethodPost, oauthPRMPath, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Errorf("POST metadata = %d %q", rec.Code, rec.Header().Get("Allow"))
	}
	rec, _ = getJSON(t, mux, oauthPRMPath, map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Origin on metadata = %d", rec.Code)
	}
}

func TestOAuth401PointsAtResourceMetadata(t *testing.T) {
	_, mux, deviceToken := newTestOAuthServer(t)
	rec, _ := getJSON(t, mux, "/mcp", nil)
	if rec.Code != 401 {
		t.Fatalf("no credential = %d", rec.Code)
	}
	want := `Bearer realm="corgi", resource_metadata="http://127.0.0.1:18765/.well-known/oauth-protected-resource/mcp"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("bare challenge = %q", got)
	}
	rec, _ = getJSON(t, mux, "/mcp", map[string]string{"Authorization": "Bearer nope"})
	if got := rec.Header().Get("WWW-Authenticate"); got != want+`, error="invalid_token"` {
		t.Errorf("challenge with credential = %q", got)
	}
	rec, _ = getJSON(t, mux, "/mcp", map[string]string{"Authorization": "Bearer " + deviceToken})
	if rec.Code != 200 {
		t.Errorf("device token still works = %d", rec.Code)
	}
}

func TestOAuthOffWithoutAuthOrWithFlag(t *testing.T) {
	// serveMCPHTTP mounts oa only with auth and without --no-oauth; the 401
	// challenge without oa keeps the pre-OAuth shape.
	rec := httptest.NewRecorder()
	bearerAuth("tok", http.NotFoundHandler(), "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="corgi"` {
		t.Errorf("challenge without oauth = %q", got)
	}
}

func postJSON(t *testing.T, h http.Handler, path, body string, hdr map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func TestOAuthRegisterShape(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	rec, body := postJSON(t, mux, oauthRegisterPath, `{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"client_name":"Claude","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	id, _ := body["client_id"].(string)
	if !strings.HasPrefix(id, "corgi_client_") || len(id) != len("corgi_client_")+43 {
		t.Errorf("client_id = %q", id)
	}
	if _, has := body["client_secret"]; has {
		t.Error("no secret for a public client")
	}
	if body["client_name"] != "Claude" || body["token_endpoint_auth_method"] != "none" || body["client_id_issued_at"] == nil {
		t.Errorf("body = %v", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("registration response must not be cached")
	}
	st, err := loadOAuthState(oa.statePath)
	if err != nil || len(st.Clients) != 1 || st.Clients[0].ID != id {
		t.Errorf("state on disk = %+v, %v", st, err)
	}
	if info, err := os.Stat(oa.statePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("oauth.json mode = %v %v", info, err)
	}
}

func TestOAuthRegisterRefusals(t *testing.T) {
	_, mux, _ := newTestOAuthServer(t)
	cases := map[string]string{
		`{"redirect_uris":["https://evil.example/cb"]}`: "invalid_redirect_uri",
		`{"redirect_uris":["http://claude.ai/cb"]}`:     "invalid_redirect_uri",
		`{"redirect_uris":[]}`:                          "invalid_redirect_uri",
		`{"redirect_uris":["http://localhost/cb"],"token_endpoint_auth_method":"client_secret_basic"}`: "invalid_client_metadata",
		`{"redirect_uris":["http://localhost/cb"],"grant_types":["client_credentials"]}`:               "invalid_client_metadata",
		`not json`: "invalid_client_metadata",
	}
	for body, want := range cases {
		rec, out := postJSON(t, mux, oauthRegisterPath, body, nil)
		if rec.Code != http.StatusBadRequest || out["error"] != want {
			t.Errorf("%s → %d %v, want %s", body, rec.Code, out, want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, oauthRegisterPath, strings.NewReader(`{"redirect_uris":["http://localhost/cb"]}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("form body on register = %d", rec.Code)
	}
}

func TestOAuthClientStoreEvictsAtCap(t *testing.T) {
	now := time.Now()
	st := &oauthState{}
	for i := 0; i < maxOAuthClients; i++ {
		st.addClient(oauthClient{ID: fmt.Sprintf("c%03d", i), CreatedAt: now.Add(time.Duration(i) * time.Second)}, now)
	}
	// c000 is the oldest but has a live grant; c001 is the oldest without.
	st.Families = []tokenFamily{{ClientID: "c000", ExpiresAt: now.Add(time.Hour)}}
	st.addClient(oauthClient{ID: "new", CreatedAt: now.Add(time.Hour)}, now)
	if len(st.Clients) != maxOAuthClients {
		t.Fatalf("len = %d", len(st.Clients))
	}
	if _, ok := st.findClient("c000"); !ok {
		t.Error("a client with a live grant must survive eviction")
	}
	if _, ok := st.findClient("c001"); ok {
		t.Error("the oldest client without a grant must go")
	}
	if _, ok := st.findClient("new"); !ok {
		t.Error("the new client must be kept")
	}
}

func TestOAuthRateBucket(t *testing.T) {
	b := rateBucket{capacity: 3, perSecond: 1}
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !b.take(now) {
			t.Fatalf("take %d refused", i)
		}
	}
	if b.take(now) {
		t.Error("4th take in the same instant must be refused")
	}
	if !b.take(now.Add(time.Second)) {
		t.Error("one second refills one token")
	}
}
