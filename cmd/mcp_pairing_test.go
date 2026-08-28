package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/tunnel"
)

func pairingFixture(t *testing.T) (*pairing.Session, string, string) {
	t.Helper()
	session, code, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	return session, code, pairing.StorePath(t.TempDir())
}

func postPair(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPairEndpointIssuesADeviceToken(t *testing.T) {
	session, code, store := pairingFixture(t)
	h := pairingHandler(session, store)

	rec := postPair(t, h, `{"code":"`+code+`","device":"my-phone"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp pairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Token, pairing.TokenPrefix) {
		t.Errorf("token = %q, want a device token", resp.Token)
	}
	if resp.Device != "my-phone" {
		t.Errorf("device = %q", resp.Device)
	}
}

// A code seen in transit must be useless afterwards.
func TestPairEndpointRefusesAReplayedCode(t *testing.T) {
	session, code, store := pairingFixture(t)
	h := pairingHandler(session, store)

	if rec := postPair(t, h, `{"code":"`+code+`","device":"first"}`); rec.Code != http.StatusOK {
		t.Fatalf("first pairing failed: %s", rec.Body)
	}

	rec := postPair(t, h, `{"code":"`+code+`","device":"attacker"}`)

	if rec.Code == http.StatusOK {
		t.Fatal("a used code must not pair a second device")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestPairEndpointRefusesAWrongCode(t *testing.T) {
	session, _, store := pairingFixture(t)
	h := pairingHandler(session, store)

	rec := postPair(t, h, `{"code":"NOTTHECODE","device":"guesser"}`)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Error("a failed attempt must not return a token")
	}
}

func TestPairEndpointClosesAfterRepeatedGuesses(t *testing.T) {
	session, code, store := pairingFixture(t)
	h := pairingHandler(session, store)

	for range pairing.MaxAttempts {
		postPair(t, h, `{"code":"WRONGWRONG","device":"guesser"}`)
	}

	rec := postPair(t, h, `{"code":"`+code+`","device":"legitimate"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("the window must stay closed after repeated wrong codes, even for the right one")
	}
}

func TestPairEndpointServesThePairPageOnGet(t *testing.T) {
	session, _, store := pairingFixture(t)
	h := pairingHandler(session, store)

	req := httptest.NewRequest(http.MethodGet, "/pair", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "device") || !strings.Contains(body, "location.hash") {
		t.Error("the pair page must ask for a device name and read the code from the URL fragment")
	}
	if strings.Contains(body, session.Code()) {
		t.Error("the pairing code must never be server-rendered into the page — it arrives only via the QR fragment")
	}
}

func TestPairEndpointGetWhenClosedSaysSoWithoutDetails(t *testing.T) {
	session, _, store := pairingFixture(t)
	session.Close()
	h := pairingHandler(session, store)

	req := httptest.NewRequest(http.MethodGet, "/pair", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a browser on a closed link must get a page, not JSON: %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/app") || !strings.Contains(body, "corgi_token") {
		t.Error("the closed page must route an already-paired browser to the launcher")
	}
	if strings.Contains(body, session.Code()) || strings.Contains(body, "expired") && strings.Contains(body, "used by") {
		t.Error("the closed page must not reveal the code or why the window closed")
	}
}

func TestPairEndpointRejectsOtherMethods(t *testing.T) {
	session, _, store := pairingFixture(t)
	h := pairingHandler(session, store)

	req := httptest.NewRequest(http.MethodPut, "/pair", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestPairEndpointRejectsAHugeBody(t *testing.T) {
	session, _, store := pairingFixture(t)
	h := pairingHandler(session, store)

	rec := postPair(t, h, `{"code":"x","device":"`+strings.Repeat("A", maxPairBodyBytes*2)+`"}`)

	if rec.Code == http.StatusOK {
		t.Fatal("an oversized body must not be accepted")
	}
}

func TestPairEndpointRejectsMalformedJSON(t *testing.T) {
	session, _, store := pairingFixture(t)
	h := pairingHandler(session, store)

	rec := postPair(t, h, `{not json`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPairEndpointRefusedWhenTheWindowIsClosed(t *testing.T) {
	session, code, store := pairingFixture(t)
	session.Close()
	h := pairingHandler(session, store)

	rec := postPair(t, h, `{"code":"`+code+`","device":"late"}`)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	// Deliberately vague: an expired window and a used one look the same.
	if strings.Contains(strings.ToLower(rec.Body.String()), "expired") {
		t.Error("the closed-window response should not distinguish why")
	}
}

// --- device-token auth ---

func pairedToken(t *testing.T, storeDir, device string) string {
	t.Helper()
	store := pairing.StorePath(storeDir)
	session, code, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	token, err := pairing.Pair(store, session, code, device)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestBearerAuthAcceptsAPairedDevice(t *testing.T) {
	dir := t.TempDir()
	token := pairedToken(t, dir, "phone")
	reached := false
	h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), pairing.StorePath(dir))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Error("a paired device's own token must be accepted, so a phone never holds the server token")
	}
}

func TestBearerAuthStillAcceptsTheServerToken(t *testing.T) {
	dir := t.TempDir()
	reached := false
	h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), pairing.StorePath(dir))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer server-token")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Error("existing clients using the server token must keep working")
	}
}

func TestBearerAuthRejectsARevokedDevice(t *testing.T) {
	dir := t.TempDir()
	token := pairedToken(t, dir, "phone")
	path := pairing.StorePath(dir)

	store, err := pairing.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Revoke("phone")
	if err := pairing.Save(path, store); err != nil {
		t.Fatal(err)
	}

	reached := false
	h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), path)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("a revoked device must lose access immediately — a lost phone is the whole reason revocation exists")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuthRejectsUnknownTokens(t *testing.T) {
	dir := t.TempDir()
	pairedToken(t, dir, "phone")

	for _, header := range []string{
		"",
		"Bearer nonsense",
		"Bearer " + pairing.TokenPrefix + "made-up",
		"Basic abc",
	} {
		reached := false
		h := bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}), pairing.StorePath(dir))
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)

		if reached {
			t.Errorf("header %q was accepted", header)
		}
	}
}

// Device tokens must not become a way to reach an endpoint the operator
// deliberately left unauthenticated-but-local. With no server token and no
// device store, the handler is passed through unchanged as before.
func TestBearerAuthUnchangedWhenNothingIsConfigured(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	h := bearerAuth("", next, "")
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if !reached {
		t.Error("no token and no device store should behave exactly as before")
	}
}

// Pairing without a server token still protects the MCP endpoint, because a
// device store is present and only paired tokens pass.
func TestDeviceStoreAloneStillGatesTheEndpoint(t *testing.T) {
	dir := t.TempDir()
	pairedToken(t, dir, "phone")

	reached := false
	h := bearerAuth("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), pairing.StorePath(dir))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if reached {
		t.Error("an unauthenticated request must not pass once a device store is in play")
	}
}

func TestDeviceStorePathIsUnderTheAgentDir(t *testing.T) {
	if got := pairing.StorePath("/data/agent"); got != filepath.Join("/data/agent", "devices.json") {
		t.Errorf("StorePath = %q", got)
	}
}

// /pair must be reachable WITHOUT a bearer token — a client that has one does
// not need to pair. Mounting it inside the bearer check returned 401 to every
// device trying to enrol, which defeats the feature entirely.
func TestPairRouteIsReachableWithoutAToken(t *testing.T) {
	session, code, store := pairingFixture(t)

	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), store))
	mux.Handle("/pair", pairingHandler(session, store))

	req := httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+code+`","device":"phone"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("pairing without a token returned %d: %s", rec.Code, rec.Body)
	}
}

// ...while /mcp beside it stays closed to an unauthenticated caller.
func TestMCPRouteStaysClosedWhilePairingIsOpen(t *testing.T) {
	session, _, store := pairingFixture(t)
	reached := false

	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), store))
	mux.Handle("/pair", pairingHandler(session, store))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if reached || rec.Code != http.StatusUnauthorized {
		t.Errorf("an open pairing window must not open the MCP endpoint: reached=%v status=%d", reached, rec.Code)
	}
}

// `corgi mcp --http` with no token is documented as unauthenticated. Consulting
// a device store that always exists silently turned that into 401 for every
// request, with no credential in existence to fix it.
func TestNoTokenAndNoPairedDevicesStaysOpen(t *testing.T) {
	dir := t.TempDir() // a store path that exists but holds nothing
	if pairing.InspectStore(pairing.StorePath(dir)) != pairing.StoreEmpty {
		t.Fatal("fixture should have no paired devices")
	}

	reached := false
	h := bearerAuth("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), "")
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if !reached {
		t.Error("an endpoint the operator deliberately left open must stay open")
	}
}

func TestInspectStore(t *testing.T) {
	dir := t.TempDir()
	path := pairing.StorePath(dir)

	if got := pairing.InspectStore(path); got != pairing.StoreEmpty {
		t.Errorf("absent store = %v, want StoreEmpty", got)
	}
	pairedToken(t, dir, "phone")
	if got := pairing.InspectStore(path); got != pairing.StoreHasDevices {
		t.Errorf("paired store = %v, want StoreHasDevices", got)
	}
}

// Collapsing "cannot read" into "no devices" would let a chmod or a truncated
// file turn an authenticated endpoint back into an open one.
func TestInspectStoreDistinguishesUnreadable(t *testing.T) {
	dir := t.TempDir()
	pairedToken(t, dir, "phone")
	path := pairing.StorePath(dir)

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pairing.InspectStore(path); got != pairing.StoreUnreadable {
		t.Errorf("world-readable store = %v, want StoreUnreadable", got)
	}

	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := pairing.InspectStore(path); got != pairing.StoreUnreadable {
		t.Errorf("corrupt store = %v, want StoreUnreadable", got)
	}
}

func TestProbeWaitsBeforeAskingDNS(t *testing.T) {
	origProbe := exposureProbe
	t.Cleanup(func() { exposureProbe = origProbe })

	var asked int
	exposureProbe = func(context.Context, string) tunnel.AccessResult {
		asked++
		return tunnel.AccessResult{Detail: "probe failed: dial tcp: lookup x: no such host"}
	}
	var waited []time.Duration
	sleep := func(context.Context, time.Duration) bool { return true }
	record := func(ctx context.Context, d time.Duration) bool {
		waited = append(waited, d)
		return sleep(ctx, d)
	}

	probeTunnelExposure(context.Background(), "https://x.trycloudflare.com/mcp", record)

	if len(waited) == 0 || waited[0] == 0 {
		t.Fatalf("the first probe must wait for DNS to propagate, waits=%v", waited)
	}
	if asked != len(probeDelays) {
		t.Errorf("an unresolved name must be retried: asked %d times, want %d", asked, len(probeDelays))
	}
	if mcpTunnelPrivate.Load() {
		t.Error("a failed probe must not mark the tunnel private")
	}
}

func TestProbeStopsOnceItGetsAnAnswer(t *testing.T) {
	origProbe := exposureProbe
	t.Cleanup(func() { exposureProbe = origProbe; mcpTunnelPrivate.Store(false) })

	var asked int
	exposureProbe = func(context.Context, string) tunnel.AccessResult {
		asked++
		return tunnel.AccessResult{Detail: "public endpoint"}
	}
	probeTunnelExposure(context.Background(), "https://x.trycloudflare.com/mcp",
		func(context.Context, time.Duration) bool { return true })
	if asked != 1 {
		t.Errorf("a real answer must end the retries, asked %d times", asked)
	}
}

func TestProbeGivesUpWhenTheServerStops(t *testing.T) {
	origProbe := exposureProbe
	t.Cleanup(func() { exposureProbe = origProbe })
	var asked int
	exposureProbe = func(context.Context, string) tunnel.AccessResult {
		asked++
		return tunnel.AccessResult{}
	}
	probeTunnelExposure(context.Background(), "https://x/mcp",
		func(context.Context, time.Duration) bool { return false })
	if asked != 0 {
		t.Errorf("a cancelled wait must not probe, asked %d times", asked)
	}
}

func TestWaitOrDoneRespectsCancellation(t *testing.T) {
	if !waitOrDone(context.Background(), time.Millisecond) {
		t.Error("a completed wait must report true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitOrDone(ctx, time.Hour) {
		t.Error("a cancelled context must not wait out the delay")
	}
}
