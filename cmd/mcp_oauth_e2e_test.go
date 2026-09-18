package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/pairing"

	"github.com/mark3labs/mcp-go/server"
)

func jsonRPC(t *testing.T, base, access, sid, body string) (int, string, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+access)
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	text := string(raw)
	if i := strings.Index(text, "data: "); i >= 0 {
		text = text[i+len("data: "):]
		if j := strings.Index(text, "\n"); j >= 0 {
			text = text[:j]
		}
	}
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), out
}

func toolCount(t *testing.T, base, access string) (int, int) {
	t.Helper()
	code, sid, _ := jsonRPC(t, base, access, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	if code != 200 {
		return code, 0
	}
	code, _, out := jsonRPC(t, base, access, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result, _ := out["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	return code, len(tools)
}

func TestOAuthEndToEndConnectFlow(t *testing.T) {
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
	mcpSrv := server.NewStreamableHTTPServer(newCorgiMCPServer("test"))
	mux.Handle("/mcp", mcpEndpointGuard(origins, bearerAuthWithOAuth("", mcpSrv, store, oa)))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oa.setIssuer(srv.URL)
	origins.add(srv.URL)

	resp, _ := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 401 {
		t.Fatalf("cold /mcp = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+srv.URL+oauthPRMPath+`/mcp"`) {
		t.Fatalf("challenge = %q", resp.Header.Get("WWW-Authenticate"))
	}
	var prm, as map[string]any
	getInto(t, srv.URL+oauthPRMPath+"/mcp", &prm)
	getInto(t, prm["authorization_servers"].([]any)[0].(string)+oauthASMetadataPath, &as)
	if as["issuer"] != srv.URL {
		t.Fatalf("issuer = %v", as["issuer"])
	}
	resp, err = http.Post(as["registration_endpoint"].(string), "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["`+testRedirectURI+`"],"token_endpoint_auth_method":"none"}`))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("register = %v %d", err, resp.StatusCode)
	}
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	clientID := reg["client_id"].(string)
	authorize := as["authorization_endpoint"].(string) + "?" + url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {testRedirectURI},
		"code_challenge": {challengeFor(testVerifier)}, "code_challenge_method": {"S256"}, "state": {"s1"},
	}.Encode()
	resp, _ = http.Get(authorize)
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("authorize = %d %s", resp.StatusCode, page)
	}
	id := regexp.MustCompile(`var id="([^"]+)"`).FindSubmatch(page)[1]
	req, _ := http.NewRequest(http.MethodPost, srv.URL+oauthApprovePath, strings.NewReader(`{"id":"`+string(id)+`"}`))
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", srv.URL)
	resp, _ = http.DefaultClient.Do(req)
	var approved map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&approved)
	if resp.StatusCode != 200 {
		t.Fatalf("approve = %d %v", resp.StatusCode, approved)
	}
	redirect, _ := url.Parse(approved["redirect"])
	if redirect.Query().Get("state") != "s1" || redirect.Query().Get("iss") != srv.URL {
		t.Fatalf("redirect = %s", redirect)
	}
	tok := postFormInto(t, as["token_endpoint"].(string), url.Values{
		"grant_type": {"authorization_code"}, "code": {redirect.Query().Get("code")}, "code_verifier": {testVerifier},
		"client_id": {clientID}, "redirect_uri": {testRedirectURI},
	})
	access1, refresh1 := tok["access_token"].(string), tok["refresh_token"].(string)
	if code, n := toolCount(t, srv.URL, access1); code != 200 || n < 50 {
		t.Fatalf("tools/list with the access token = %d, %d tools", code, n)
	}
	tok2 := postFormInto(t, as["token_endpoint"].(string), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh1}, "client_id": {clientID}})
	access2 := tok2["access_token"].(string)
	if code, _ := toolCount(t, srv.URL, access1); code != 401 {
		t.Error("old access token must be dead after refresh")
	}
	if code, n := toolCount(t, srv.URL, access2); code != 200 || n < 50 {
		t.Errorf("new access token = %d, %d tools", code, n)
	}
	errResp := postFormInto(t, as["token_endpoint"].(string), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh1}})
	if errResp["error"] != "invalid_grant" {
		t.Errorf("replay = %v", errResp)
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+access2)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("after replay the live access token = %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	s, _ := pairing.Load(store)
	for _, d := range s.Devices {
		if d.Family != "" {
			t.Errorf("oauth device survived: %s", d.Name)
		}
	}
	if _, ok := s.Find("laptop browser"); !ok {
		t.Error("the paired browser must be untouched")
	}
}

func getInto(t *testing.T, u string, into any) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET %s: %v %v", u, err, resp)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

func postFormInto(t *testing.T, u string, form url.Values) map[string]any {
	t.Helper()
	resp, err := http.PostForm(u, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}
