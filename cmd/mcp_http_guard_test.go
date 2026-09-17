package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func guardedRecorder(t *testing.T, allow *originAllowlist, method string, headers map[string]string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	reached := false
	h := mcpEndpointGuard(allow, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	req := httptest.NewRequest(method, "/mcp", strings.NewReader("{}"))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, reached
}

func TestGuardRefusesStrayMethodsWith405(t *testing.T) {
	allow := newOriginAllowlist("127.0.0.1:8765", nil)
	for _, m := range []string{http.MethodPut, http.MethodPatch, http.MethodHead} {
		rec, reached := guardedRecorder(t, allow, m, nil)
		if reached || rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d reached=%v, want 405", m, rec.Code, reached)
		}
		if got := rec.Header().Get("Allow"); !strings.Contains(got, "POST") || !strings.Contains(got, "GET") {
			t.Errorf("%s: Allow header %q must name POST and GET", m, got)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		if _, reached := guardedRecorder(t, allow, m, nil); !reached {
			t.Errorf("%s must pass the method check", m)
		}
	}
}

func TestGuardOriginPolicy(t *testing.T) {
	allow := newOriginAllowlist("127.0.0.1:8765", []string{"https://app.example.com"})
	cases := []struct {
		origin string
		pass   bool
	}{
		{"", true},
		{"http://127.0.0.1:8765", true},
		{"http://127.0.0.1:5173", true},
		{"http://localhost:3000", true},
		{"http://[::1]:8765", true},
		{"https://app.example.com", true},
		{"https://APP.example.com", true},
		{"http://evil.example", false},
		{"https://app.example.com.evil.example", false},
		{"https://evil.example/https://app.example.com", false},
		{"null", false},
		{"http://localhost.evil.example", false},
	}
	for _, c := range cases {
		headers := map[string]string{}
		if c.origin != "" {
			headers["Origin"] = c.origin
		}
		rec, reached := guardedRecorder(t, allow, http.MethodPost, headers)
		if c.pass && !reached {
			t.Errorf("origin %q: refused with %d, want pass", c.origin, rec.Code)
		}
		if !c.pass && (reached || rec.Code != http.StatusForbidden) {
			t.Errorf("origin %q: got %d reached=%v, want 403", c.origin, rec.Code, reached)
		}
	}
}

func TestGuardOriginLearnsTheTunnelLater(t *testing.T) {
	allow := newOriginAllowlist("127.0.0.1:8765", nil)
	headers := map[string]string{"Origin": "https://abc.trycloudflare.com"}
	if rec, reached := guardedRecorder(t, allow, http.MethodPost, headers); reached || rec.Code != http.StatusForbidden {
		t.Fatalf("unknown tunnel origin must be refused first, got %d", rec.Code)
	}
	allow.add("https://abc.trycloudflare.com/mcp")
	if _, reached := guardedRecorder(t, allow, http.MethodPost, headers); !reached {
		t.Error("once the tunnel URL is known its origin must pass")
	}
}

func TestGuardProtocolVersionHeader(t *testing.T) {
	allow := newOriginAllowlist("127.0.0.1:8765", nil)
	for _, v := range []string{"", "2025-03-26", "2025-06-18", "2025-11-25"} {
		headers := map[string]string{}
		if v != "" {
			headers["MCP-Protocol-Version"] = v
		}
		if rec, reached := guardedRecorder(t, allow, http.MethodPost, headers); !reached {
			t.Errorf("version %q: refused with %d, want pass", v, rec.Code)
		}
	}
	for _, v := range []string{"1999-01-01", "latest", "2025-06-18; charset=x"} {
		rec, reached := guardedRecorder(t, allow, http.MethodPost, map[string]string{"MCP-Protocol-Version": v})
		if reached || rec.Code != http.StatusBadRequest {
			t.Errorf("version %q: got %d reached=%v, want 400", v, rec.Code, reached)
			continue
		}
		var body struct {
			JSONRPC string `json:"jsonrpc"`
			Error   struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.JSONRPC != "2.0" || body.Error.Code == 0 {
			t.Errorf("version %q: body must be a JSON-RPC error, got %q", v, rec.Body.String())
		}
	}
}

func TestBearerAuth401CarriesWWWAuthenticate(t *testing.T) {
	dir := t.TempDir()
	pairedToken(t, dir, "phone")
	cases := []struct {
		header string
		want   string
	}{
		{"", `Bearer realm="corgi"`},
		{"Bearer nonsense", `Bearer realm="corgi", error="invalid_token"`},
		{"Bearer " + pairing.TokenPrefix + "made-up", `Bearer realm="corgi", error="invalid_token"`},
		{"Basic abc", `Bearer realm="corgi", error="invalid_token"`},
	}
	for _, c := range cases {
		h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Errorf("header %q was accepted", c.header)
		}), pairing.StorePath(dir))
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status %d, want 401", c.header, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != c.want {
			t.Errorf("header %q: WWW-Authenticate %q, want %q", c.header, got, c.want)
		}
	}
}

func TestBearerAuthOnlyCorgiTokens(t *testing.T) {
	dir := t.TempDir()
	device := pairedToken(t, dir, "phone")
	pass := map[string]bool{
		"Bearer server-token": true,
		"Bearer " + device:    true,
		"Bearer eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJjb3JnaSJ9.sig":               false,
		"Bearer corgi_mcp_notthisone":                                        false,
		"Bearer " + pairing.TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA": false,
		"Bearer server-token ":                                               false,
		"bearer server-token":                                                false,
	}
	for header, want := range pass {
		reached := false
		h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), pairing.StorePath(dir))
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", header)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if reached != want {
			t.Errorf("header %q: reached=%v, want %v", header, reached, want)
		}
	}
}

func TestResolveMCPListenAddr(t *testing.T) {
	cases := []struct {
		in      string
		bindAll bool
		want    string
		notice  bool
		err     bool
	}{
		{":8765", false, "127.0.0.1:8765", true, false},
		{"8765", false, "127.0.0.1:8765", true, false},
		{"127.0.0.1:8765", false, "127.0.0.1:8765", false, false},
		{"localhost:8765", false, "localhost:8765", false, false},
		{"[::1]:8765", false, "[::1]:8765", false, false},
		{"0.0.0.0:8765", false, "", false, true},
		{"192.168.1.5:8765", false, "", false, true},
		{"[::]:8765", false, "", false, true},
		{":8765", true, ":8765", false, false},
		{"0.0.0.0:8765", true, "0.0.0.0:8765", false, false},
		{"nonsense", false, "", false, true},
	}
	for _, c := range cases {
		got, notice, err := resolveMCPListenAddr(c.in, c.bindAll)
		if (err != nil) != c.err {
			t.Errorf("%q bindAll=%v: err=%v, want err=%v", c.in, c.bindAll, err, c.err)
			continue
		}
		if got != c.want {
			t.Errorf("%q bindAll=%v: addr %q, want %q", c.in, c.bindAll, got, c.want)
		}
		if (notice != "") != c.notice {
			t.Errorf("%q bindAll=%v: notice %q, want notice=%v", c.in, c.bindAll, notice, c.notice)
		}
	}
}
