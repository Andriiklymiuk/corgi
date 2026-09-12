package cmd

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
