package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func registerTestClient(t *testing.T, oa *oauthServer) {
	t.Helper()
	oa.mu.Lock()
	defer oa.mu.Unlock()
	if _, ok := oa.state.findClient(testClientID); !ok {
		oa.state.addClient(oauthClient{ID: testClientID, Name: "Claude", RedirectURIs: []string{testRedirectURI, "http://localhost/callback"}, CreatedAt: oa.now()}, oa.now())
	}
}

func authorizeQuery(overrides map[string]string) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {testClientID}, "redirect_uri": {testRedirectURI},
		"code_challenge": {challengeFor(testVerifier)}, "code_challenge_method": {"S256"}, "state": {"xyz"},
	}
	for k, v := range overrides {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	return oauthAuthorizePath + "?" + q.Encode()
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var pendingIDRe = regexp.MustCompile(`var id="([^"]+)"`)
var approveCodeRe = regexp.MustCompile(`corgi agent approve ([A-Z2-9]{4}-[A-Z2-9]{4})`)

func TestOAuthAuthorizeNeverRedirectsAnUntrustedTarget(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	registerTestClient(t, oa)
	for name, q := range map[string]string{
		"unknown client":    authorizeQuery(map[string]string{"client_id": "corgi_client_nobody"}),
		"foreign redirect":  authorizeQuery(map[string]string{"redirect_uri": "https://evil.example/cb"}),
		"unregistered path": authorizeQuery(map[string]string{"redirect_uri": "https://claude.ai/other"}),
		"missing redirect":  authorizeQuery(map[string]string{"redirect_uri": ""}),
	} {
		rec := get(mux, q)
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d Location=%q — must be a 400 page, never a redirect", name, rec.Code, rec.Header().Get("Location"))
		}
		if !strings.Contains(rec.Body.String(), "cannot start this sign-in") {
			t.Errorf("%s: body is not the error page", name)
		}
	}
}

func TestOAuthAuthorizeErrorsTravelToATrustedRedirect(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	registerTestClient(t, oa)
	cases := map[string]struct {
		q    string
		want string
	}{
		"token response": {authorizeQuery(map[string]string{"response_type": "token"}), "unsupported_response_type"},
		"plain pkce":     {authorizeQuery(map[string]string{"code_challenge_method": "plain"}), "invalid_request"},
		"no challenge":   {authorizeQuery(map[string]string{"code_challenge": ""}), "invalid_request"},
	}
	for name, c := range cases {
		rec := get(mux, c.q)
		loc, _ := url.Parse(rec.Header().Get("Location"))
		if rec.Code != http.StatusFound || loc == nil || loc.Host != "claude.ai" {
			t.Fatalf("%s: %d %q", name, rec.Code, rec.Header().Get("Location"))
		}
		if loc.Query().Get("error") != c.want || loc.Query().Get("state") != "xyz" || loc.Query().Get("iss") != "http://127.0.0.1:18765" {
			t.Errorf("%s: %s", name, loc.RawQuery)
		}
	}
}

func TestOAuthConsentPageAndSameBrowserApprove(t *testing.T) {
	oa, mux, deviceToken := newTestOAuthServer(t)
	registerTestClient(t, oa)
	rec := get(mux, authorizeQuery(map[string]string{"redirect_uri": "http://localhost:51234/callback"}))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("consent = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{"Connect <span id=\"client\">Claude</span>", "localhost", "a program on your computer", "corgi agent approve "} {
		if !strings.Contains(body, want) {
			t.Errorf("consent page lacks %q", want)
		}
	}
	m := pendingIDRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("pending id not in page")
	}
	id := m[1]

	// a viewer cannot approve
	viewerStore, _ := pairing.Load(oa.deviceStore)
	viewerTok, _ := pairing.NewDeviceToken()
	viewerStore.Devices = append(viewerStore.Devices, pairing.Device{Name: "teammate", TokenHash: pairing.HashToken(viewerTok), Role: pairing.RoleViewer})
	_ = pairing.Save(oa.deviceStore, viewerStore)
	rec, _ = postJSON(t, mux, oauthApprovePath, `{"id":"`+id+`"}`, map[string]string{"Authorization": "Bearer " + viewerTok})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("viewer approve = %d", rec.Code)
	}
	rec, _ = postJSON(t, mux, oauthApprovePath, `{"id":"`+id+`"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous approve = %d", rec.Code)
	}

	rec, out := postJSON(t, mux, oauthApprovePath, `{"id":"`+id+`"}`, map[string]string{"Authorization": "Bearer " + deviceToken, "Origin": "http://127.0.0.1:18765"})
	if rec.Code != 200 || out["status"] != "approved" || out["client"] != "Claude" {
		t.Fatalf("approve = %d %v", rec.Code, out)
	}
	redirect, _ := url.Parse(out["redirect"].(string))
	if redirect.Host != "localhost:51234" || redirect.Path != "/callback" {
		t.Errorf("redirect goes to %s — the requested loopback port must be kept", redirect)
	}
	code := redirect.Query().Get("code")
	if len(code) != 43 || redirect.Query().Get("state") != "xyz" || redirect.Query().Get("iss") != "http://127.0.0.1:18765" {
		t.Errorf("redirect query = %s", redirect.RawQuery)
	}
	if rec = get(mux, oauthPendingPath+id); rec.Code != http.StatusNotFound {
		t.Error("a pending entry approved from the page is consumed")
	}

	form := codeGrant(code)
	form.Set("redirect_uri", "http://localhost:51234/callback")
	rec, tok := postForm(t, mux, oauthTokenPath, form)
	if rec.Code != 200 || tok["access_token"] == nil {
		t.Fatalf("token after consent = %d %v", rec.Code, tok)
	}
	rec, _ = postJSON(t, mux, oauthApprovePath, `{"id":"`+id+`"}`, map[string]string{"Authorization": "Bearer " + deviceToken, "Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Origin on approve = %d", rec.Code)
	}
}

func TestOAuthApproveByCodeAndPolling(t *testing.T) {
	oa, mux, deviceToken := newTestOAuthServer(t)
	registerTestClient(t, oa)
	body := get(mux, authorizeQuery(nil)).Body.String()
	id := pendingIDRe.FindStringSubmatch(body)[1]
	approveCode := approveCodeRe.FindStringSubmatch(body)[1]

	if rec := get(mux, oauthPendingPath+id); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("poll before approval = %d %s", rec.Code, rec.Body.String())
	}
	rec, out := postJSON(t, mux, oauthApprovePath, `{"code":"`+strings.ToLower(approveCode)+`"}`, map[string]string{"Authorization": "Bearer " + deviceToken})
	if rec.Code != 200 || out["client"] != "Claude" {
		t.Fatalf("approve by code = %d %v", rec.Code, out)
	}
	rec = get(mux, oauthPendingPath+id)
	var poll map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &poll)
	if poll["status"] != "approved" || !strings.Contains(poll["redirect"], "code=") {
		t.Fatalf("poll after approval = %v", poll)
	}
	if rec = get(mux, oauthPendingPath+id); rec.Code != http.StatusNotFound {
		t.Error("a polled approval is consumed")
	}
	rec, out = postJSON(t, mux, oauthApprovePath, `{"code":"ZZZZ-2222"}`, map[string]string{"Authorization": "Bearer " + deviceToken})
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown code = %d %v", rec.Code, out)
	}
}

func TestOAuthPendingCap(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	registerTestClient(t, oa)
	oa.bucket.capacity = 1000
	oa.bucket.tokens = 1000
	for i := 0; i < maxPendingAuths; i++ {
		if rec := get(mux, authorizeQuery(nil)); rec.Code != 200 {
			t.Fatalf("authorize %d = %d", i, rec.Code)
		}
	}
	rec := get(mux, authorizeQuery(nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Query().Get("error") != "temporarily_unavailable" {
		t.Errorf("21st pending = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOAuthAuthorizeWithCIMDClient(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	f, _ := cimdTestServer(t, goodCIMD)
	oa.cimd = f
	rec := get(mux, authorizeQuery(map[string]string{"client_id": testCIMDID, "redirect_uri": "http://127.0.0.1:60000/callback"}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Claude Code") {
		t.Errorf("CIMD authorize = %d", rec.Code)
	}
	rec = get(mux, authorizeQuery(map[string]string{"client_id": testCIMDID, "redirect_uri": "https://claude.ai/api/mcp/auth_callback"}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a redirect the document does not list = %d", rec.Code)
	}
}

func TestAgentApproveTalksToTheLocalServer(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	registerTestClient(t, oa)
	body := get(mux, authorizeQuery(nil)).Body.String()
	approveCode := approveCodeRe.FindStringSubmatch(body)[1]

	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := tempAgentDirOf(oa)
	if err := os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(strings.TrimPrefix(srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := approvePendingLocally(dir, approveCode)
	if err != nil || client != "Claude" {
		t.Fatalf("approve = %q, %v", client, err)
	}
	store, _ := pairing.Load(oa.deviceStore)
	if _, ok := store.Find("corgi agent approve"); ok {
		t.Error("the temporary approval device must be revoked afterwards")
	}
	// approving twice is idempotent: the same redirect, no second code
	if _, err := approvePendingLocally(dir, approveCode); err != nil {
		t.Errorf("second approve = %v", err)
	}
	oa.mu.Lock()
	if len(oa.codes) != 1 {
		t.Errorf("codes minted = %d, want 1", len(oa.codes))
	}
	oa.mu.Unlock()
}
