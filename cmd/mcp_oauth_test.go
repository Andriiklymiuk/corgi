package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	oa := newOAuthServer("127.0.0.1:18765", newOAuthClientHosts(nil, nil), origins, store)
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
