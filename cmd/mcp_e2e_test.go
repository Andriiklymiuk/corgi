package cmd

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

// A phone that pairs with a key gets the machine's back, and from then on
// its bodies travel sealed both ways; plaintext from it is refused, while a
// key-less device (the web page) keeps talking as before.
func TestLaunchAuthSealsBothWaysForAKeyedDevice(t *testing.T) {
	session, code, store := pairingFixture(t)
	phone, _ := ecdh.X25519().GenerateKey(rand.Reader)

	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandler(session, store))
	mux.Handle("/launch/answer", launchAuth("server-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", mimeJSON)
		_, _ = w.Write([]byte(`{"got":` + string(body) + `}`))
	}), store))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+code+`","device":"phone","pubKey":"`+pairing.PublicKeyString(phone.PublicKey())+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	var paired struct {
		Token        string `json:"token"`
		ServerPubKey string `json:"serverPubKey"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	if paired.ServerPubKey == "" {
		t.Fatal("the machine answers a key with its own")
	}
	serverPub, err := pairing.ParsePublicKey(paired.ServerPubKey)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := pairing.SharedKeyOnDevice(phone, serverPub)

	// Plaintext from this device is refused.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/launch/answer", strings.NewReader(`{"answer":"allow"}`))
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("plaintext from a keyed device: %d %s", rec.Code, rec.Body)
	}

	// Sealed goes through, and the answer comes back sealed.
	sealed, _ := pairing.Seal(key, "POST", "/launch/answer", []byte(`{"answer":"allow"}`), time.Now())
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/launch/answer", strings.NewReader(string(sealed)))
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	req.Header.Set(pairing.E2EHeader, "1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get(pairing.E2EHeader) != "1" {
		t.Fatalf("sealed: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "allow") {
		t.Fatal("the answer must not be readable on the wire")
	}
	plain, err := pairing.Open(key, "POST", "/launch/answer", rec.Body.Bytes(), time.Now())
	if err != nil || string(plain) != `{"got":{"answer":"allow"}}` {
		t.Fatalf("the phone opens the answer: %q %v", plain, err)
	}

	// A sealed body aimed at another path does not open here.
	wrong, _ := pairing.Seal(key, "POST", "/launch/send", []byte(`{"answer":"allow"}`), time.Now())
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/launch/answer", strings.NewReader(string(wrong)))
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	req.Header.Set(pairing.E2EHeader, "1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a message for another path: %d", rec.Code)
	}

	// The server token is untouched by any of this.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/launch/answer", strings.NewReader(`{"answer":"deny"}`))
	req.Header.Set("Authorization", "Bearer server-token")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"got":{"answer":"deny"}}` {
		t.Fatalf("server token: %d %s", rec.Code, rec.Body)
	}
}

// A device that paired without a key talks plainly, and is told so if it
// starts sending envelopes.
func TestLaunchAuthLeavesAKeylessDevicePlain(t *testing.T) {
	session, code, store := pairingFixture(t)
	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandler(session, store))
	mux.Handle("/launch/info", launchAuth("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), store))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+code+`","device":"browser"}`)))
	var paired struct {
		Token        string `json:"token"`
		ServerPubKey string `json:"serverPubKey"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	if paired.ServerPubKey != "" {
		t.Fatal("no key offered, none answered")
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/launch/info", nil)
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("plain device: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/launch/info", nil)
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	req.Header.Set(pairing.E2EHeader, "1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("envelopes from a keyless device: %d", rec.Code)
	}
}

// A window opened with --viewer pairs a device that only reads: the board
// answers, a transcript and every POST are refused, and the pairing answer
// says so, so the app hides its buttons.
func TestAViewerDeviceOnlyReadsTheBoard(t *testing.T) {
	session, code, store := pairingFixture(t)
	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandlerWithRole(session, store, pairing.RoleViewer))
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	for _, p := range []string{"/launch/board", "/launch/transcript", "/launch/answer", "/launch/doctor", "/launch/sessions", "/launch/diff", "/launch/run"} {
		mux.Handle(p, launchAuth("", ok, store))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+code+`","device":"teammate"}`)))
	var paired struct {
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	if paired.Token == "" || paired.Role != "viewer" {
		t.Fatalf("paired as a viewer: %s", rec.Body)
	}
	try := func(method, path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+paired.Token)
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if try(http.MethodGet, "/launch/board") != 200 {
		t.Fatal("the board reads")
	}
	if try(http.MethodGet, "/launch/transcript") != 403 || try(http.MethodPost, "/launch/answer") != 403 || try(http.MethodGet, "/launch/doctor") != 403 || try(http.MethodGet, "/launch/sessions") != 403 {
		t.Fatal("a transcript, the doctor, the session links and a button are refused")
	}
	if try(http.MethodGet, "/launch/diff") != 403 || try(http.MethodGet, "/launch/run") != 403 {
		t.Fatal("a patch and a run's output are the laptop's own business")
	}
	devices, _ := pairing.Load(store)
	if len(devices.Devices) != 1 || !devices.Devices[0].Viewer() {
		t.Fatalf("the store says viewer: %+v", devices.Devices)
	}
}

// A window reopens on request while the server runs: the request file is
// answered with a fresh code, the old window closes, and the new code
// pairs — for a viewer when asked.
func TestAPairingWindowReopensOnRequest(t *testing.T) {
	session, oldCode, store := pairingFixture(t)
	dir := filepath.Dir(store)
	window := &pairWindow{}
	window.set(session, "")
	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandlerFor(window, store))
	go watchPairRequests(dir, window)
	ans, err := requestPairWindow(dir, true, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Code == "" || ans.Code == oldCode || ans.Role != "viewer" || ans.Daemon == "" || ans.ExpiresAt.Before(time.Now()) {
		t.Fatalf("%+v", ans)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+oldCode+`","device":"late"}`)))
	if rec.Code == 200 {
		t.Fatal("the old code died with its window")
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+ans.Code+`","device":"teammate"}`)))
	var paired struct {
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	if rec.Code != 200 || paired.Token == "" || paired.Role != "viewer" {
		t.Fatalf("the new code pairs a viewer: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, pairRequestName)); err == nil {
		t.Fatal("the request is gone once answered")
	}
}

func keyedPhone(t *testing.T, mux *http.ServeMux, session *pairing.Session, code, store string) (string, []byte) {
	t.Helper()
	phone, _ := ecdh.X25519().GenerateKey(rand.Reader)
	mux.Handle("/pair", pairingHandler(session, store))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(`{"code":"`+code+`","device":"phone","pubKey":"`+pairing.PublicKeyString(phone.PublicKey())+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	var paired struct {
		Token        string `json:"token"`
		ServerPubKey string `json:"serverPubKey"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	serverPub, err := pairing.ParsePublicKey(paired.ServerPubKey)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := pairing.SharedKeyOnDevice(phone, serverPub)
	return paired.Token, key
}

func TestAKeyedDeviceMustSealEvenABodylessChange(t *testing.T) {
	session, code, store := pairingFixture(t)
	mux := http.NewServeMux()
	reached := 0
	mux.Handle("/launch/devices", launchAuth("server-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		_, _ = w.Write([]byte(`{"done":"revoked"}`))
	}), store))
	token, key := keyedPhone(t, mux, session, code, store)

	req := httptest.NewRequest(http.MethodDelete, "/launch/devices?name=owner", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(pairing.E2EHeader, "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || reached != 0 {
		t.Fatalf("a bodyless DELETE from a keyed device: %d reached=%d", rec.Code, reached)
	}

	sealed, _ := pairing.Seal(key, "DELETE", "/launch/devices", []byte(`{}`), time.Now())
	req = httptest.NewRequest(http.MethodDelete, "/launch/devices?name=owner", strings.NewReader(string(sealed)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(pairing.E2EHeader, "1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || reached != 1 {
		t.Fatalf("a sealed DELETE: %d reached=%d %s", rec.Code, reached, rec.Body)
	}

	req = httptest.NewRequest(http.MethodGet, "/launch/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(pairing.E2EHeader, "1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || reached != 2 {
		t.Fatalf("a bodyless GET: %d reached=%d", rec.Code, reached)
	}
}

func TestASealedMessageIsDeliveredOnce(t *testing.T) {
	session, code, store := pairingFixture(t)
	mux := http.NewServeMux()
	reached := 0
	mux.Handle("/launch/send", launchAuth("server-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		_, _ = w.Write([]byte(`{"done":"sent"}`))
	}), store))
	token, key := keyedPhone(t, mux, session, code, store)
	sealed, _ := pairing.Seal(key, "POST", "/launch/send", []byte(`{"text":"rm -rf"}`), time.Now())
	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/launch/send", strings.NewReader(string(sealed)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(pairing.E2EHeader, "1")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if send() != http.StatusOK || reached != 1 {
		t.Fatal("the first delivery goes through")
	}
	if send() != http.StatusBadRequest || reached != 1 {
		t.Fatalf("the same envelope again is a replay: reached=%d", reached)
	}
}

func TestAKeyedPhoneMintsAConnectorTokenThatOpensMCP(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	agentD, _ := agentDir()
	_ = os.MkdirAll(agentD, 0o700)
	store := pairing.StorePath(agentD)
	session, code, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/launch/connector", launchAuth("server-token", http.HandlerFunc(launchConnectorHandler), store))
	reached := false
	mux.Handle("/mcp", bearerAuth("server-token", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), store))
	token, key := keyedPhone(t, mux, session, code, store)

	sealed, _ := pairing.Seal(key, "POST", "/launch/connector", []byte(`{}`), time.Now())
	req := httptest.NewRequest(http.MethodPost, "/launch/connector", strings.NewReader(string(sealed)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(pairing.E2EHeader, "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint: %d %s", rec.Code, rec.Body)
	}
	plain, err := pairing.Open(key, "POST", "/launch/connector", rec.Body.Bytes(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var minted struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(plain, &minted)
	if minted.Name != "phone · connector" || !strings.HasPrefix(minted.Token, pairing.TokenPrefix) {
		t.Fatalf("a connector token named after the phone: %s", plain)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+minted.Token)
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if !reached {
		t.Fatal("the connector token opens /mcp")
	}
	reached = false
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if reached {
		t.Fatal("the phone's own token still does not")
	}
}
