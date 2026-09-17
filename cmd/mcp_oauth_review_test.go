package cmd

import (
	"net"
	"os"
	"strings"
	"testing"
	"unicode"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func TestConsentPagesRefuseFraming(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	registerTestClient(t, oa)
	for name, path := range map[string]string{
		"consent": authorizeQuery(nil),
		"error":   authorizeQuery(map[string]string{"client_id": "corgi_client_nobody"}),
	} {
		rec := get(mux, path)
		if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s page can be framed: %v", name, rec.Header())
		}
	}
}

func TestClientNameIsSafeForATerminal(t *testing.T) {
	esc := string(rune(0x1b))
	cases := map[string]string{
		"Claude":                          "Claude",
		"  Claude   Desktop \n":           "Claude Desktop",
		"evil" + esc + "[31mred":          "evil[31mred",
		"line\nbreak\ttab":                "line break tab",
		"":                                "MCP client",
		string(rune(0)) + string(rune(7)): "MCP client",
		strings.Repeat("é", 70):           strings.Repeat("é", 64),
		strings.Repeat("🐕", 65):           strings.Repeat("🐕", 64),
	}
	for in, want := range cases {
		got := cleanClientName(in, "MCP client")
		if got != want {
			t.Errorf("cleanClientName(%q) = %q, want %q", in, got, want)
		}
		for _, r := range got {
			if r != ' ' && !unicode.IsGraphic(r) {
				t.Errorf("%q still has a non-graphic rune", got)
			}
		}
	}
}

func TestRegisteredClientNameReachesTheDeviceStoreClean(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	rec, body := postJSON(t, mux, oauthRegisterPath, `{"redirect_uris":["`+testRedirectURI+`"],"client_name":"Bad\u001b[2Jname"}`, nil)
	if rec.Code != 201 || body["client_name"] != "Bad[2Jname" {
		t.Fatalf("register = %d %v", rec.Code, body)
	}
	oa.mu.Lock()
	code, err := oa.issueCodeLocked(oauthClient{ID: body["client_id"].(string), Name: body["client_name"].(string)}, testRedirectURI, challengeFor(testVerifier))
	oa.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	form := codeGrant(code)
	form.Set("client_id", body["client_id"].(string))
	if rec, out := postForm(t, mux, oauthTokenPath, form); rec.Code != 200 {
		t.Fatalf("token = %d %v", rec.Code, out)
	}
	store, _ := pairing.Load(oa.deviceStore)
	for _, d := range store.Devices {
		for _, r := range d.Name {
			if !unicode.IsGraphic(r) {
				t.Errorf("device name %q carries a control character", d.Name)
			}
		}
	}
}

func TestApproveCodesSurviveNormalize(t *testing.T) {
	for _, r := range approveCodeAlphabet {
		if pairing.NormalizeCode(string(r)) != string(r) {
			t.Errorf("%q would be stripped by pairing.NormalizeCode", r)
		}
	}
}

func TestPublicIPReservedRanges(t *testing.T) {
	for _, s := range []string{"100.64.0.1", "100.127.255.254", "198.18.0.1", "192.0.0.9", "240.0.0.1", "255.255.255.255", "0.1.2.3", "::ffff:100.64.0.1"} {
		if publicIP(net.ParseIP(s)) {
			t.Errorf("%s must not count as public", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "160.79.104.10", "2606:4700::1111", "100.128.0.1", "198.20.0.1"} {
		if !publicIP(net.ParseIP(s)) {
			t.Errorf("%s is public", s)
		}
	}
}

func TestOAuthStateRefusesALooseFile(t *testing.T) {
	dir := t.TempDir()
	path := oauthStatePath(dir)
	if err := saveOAuthState(path, &oauthState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOAuthState(path); err != nil {
		t.Fatalf("0600 file must load: %v", err)
	}
	_ = os.Chmod(path, 0o644)
	if _, err := loadOAuthState(path); err == nil || !strings.Contains(err.Error(), "readable by others") {
		t.Errorf("0644 file must be refused, got %v", err)
	}
}
