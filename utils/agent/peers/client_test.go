package peers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func sealedPeer(t *testing.T, status int, answer any) (*Client, *string) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	got := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		plain, err := pairing.Open(key, r.Method, r.URL.Path, raw, time.Now())
		if err != nil || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		*got = string(plain)
		out, _ := json.Marshal(answer)
		sealed, _ := pairing.Seal(key, r.Method, r.URL.Path, out, time.Now())
		w.Header().Set(pairing.E2EHeader, "1")
		w.WriteHeader(status)
		_, _ = w.Write(sealed)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Peer{Name: "other", URL: srv.URL + "/", Token: "tok", Key: base64.StdEncoding.EncodeToString(key)}), got
}

func TestAPulseGoesSealedAndComesBackOpened(t *testing.T) {
	c, got := sealedPeer(t, http.StatusOK, Pulse{Name: "other", Watches: []string{"linear/api"}, Lead: true, At: 1})
	var back Pulse
	if err := c.Call(context.Background(), "/launch/peers/pulse", Pulse{Name: "me", Budget: 40}, &back); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*got, `"budget":40`) || back.Name != "other" || !back.Lead {
		t.Fatalf("sent %s, heard %+v", *got, back)
	}
	if err := c.Call(context.Background(), "/launch/peers/pulse", Pulse{Name: "me"}, nil); err != nil {
		t.Fatalf("no answer wanted: %v", err)
	}
}

func TestAPeerRefusalSaysWhy(t *testing.T) {
	c, _ := sealedPeer(t, http.StatusForbidden, map[string]string{"error": "not a peer"})
	if err := c.Call(context.Background(), "/launch/peers/pulse", Pulse{}, nil); err == nil || !strings.Contains(err.Error(), "other: not a peer") {
		t.Fatalf("%v", err)
	}
	c, _ = sealedPeer(t, http.StatusBadGateway, map[string]string{})
	if err := c.Call(context.Background(), "/launch/peers/pulse", Pulse{}, nil); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("%v", err)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer plain.Close()
	c.Peer.URL = plain.URL
	if err := c.Call(context.Background(), "/x", Pulse{}, nil); err == nil || !strings.Contains(err.Error(), "answered 404") {
		t.Fatalf("%v", err)
	}
	c.Peer.Key = "short"
	if err := c.Call(context.Background(), "/x", Pulse{}, nil); err == nil || !strings.Contains(err.Error(), "pair again") {
		t.Fatalf("%v", err)
	}
}

func TestJoinPairsWithALaptopAndRefusesAPhoneWindow(t *testing.T) {
	theirs := t.TempDir()
	priv, err := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(theirs))
	if err != nil {
		t.Fatal(err)
	}
	role := Role
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/pair" || in["code"] == "" || in["pubKey"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad code"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok", "daemon": "studio", "version": "2.32.1", "serverPubKey": pairing.PublicKeyString(priv.PublicKey()), "role": role})
	}))
	defer srv.Close()
	mine := t.TempDir()
	p, err := Join(context.Background(), mine, srv.URL+"/app?x=1", "ab-cd")
	if err != nil || p.Name != "studio" || p.Token != "tok" || p.URL != srv.URL {
		t.Fatalf("joined: %+v %v", p, err)
	}
	if s, _ := Load(Path(mine)); len(s.Peers) != 1 || s.Peers[0].Name != "studio" {
		t.Fatalf("stored: %+v", s)
	}
	if pub, err := PublicKeyOf(mine); err != nil || pub == nil {
		t.Fatalf("own key: %v", err)
	}
	role = "phone"
	if _, err := Join(context.Background(), mine, srv.URL, "ab-cd"); err == nil || !strings.Contains(err.Error(), "corgi agent peers invite") {
		t.Fatalf("a phone's window is not for a laptop: %v", err)
	}
	if _, err := Join(context.Background(), mine, srv.URL, ""); err == nil || !strings.Contains(err.Error(), "bad code") {
		t.Fatalf("a refusal names the reason: %v", err)
	}
	if _, err := Join(context.Background(), mine, "ftp://x", "c"); err == nil {
		t.Fatal("not a laptop's address")
	}
}

func TestAPeersPulseIsRecordedAndListedWithoutSecrets(t *testing.T) {
	now := time.Now()
	var p Peer
	runs := make([]PeerRun, 60)
	p.Absorb(Pulse{Name: "b", Watches: []string{"linear/api"}, Lead: true, Version: "2.32.1", Budget: 30, Unwell: "limit", Runs: runs, MutedUntil: 5}, now)
	if !p.Alive(now) || !p.Lead || p.Budget != 30 || len(p.Runs) != 50 || p.MutedUntil != 5 {
		t.Fatalf("absorbed: %+v", p)
	}
	p.Token, p.Key = "secret", "key"
	pub := p.Public()
	for _, k := range []string{"token", "key"} {
		if _, ok := pub[k]; ok {
			t.Fatalf("%s leaks into the listing", k)
		}
	}
	if pub["alive"] != true || pub["unwell"] != "limit" {
		t.Fatalf("listing: %v", pub)
	}
	if Me() == "" {
		t.Fatal("a laptop always has a name")
	}
	path := Path(t.TempDir())
	if err := Save(path, &Store{Peers: []Peer{{Name: "b"}}}); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(path)
	s.put(Peer{Name: "B", URL: "https://new"})
	if len(s.Peers) != 1 || s.Peers[0].URL != "https://new" {
		t.Fatalf("a peer by the same name is replaced: %+v", s.Peers)
	}
	if got := LeaderAmong(&Store{Peers: []Peer{{Name: "a", SeenAt: now, Watches: []string{"x"}, Budget: 20}}}, "z", []string{"x"}, 80, "", now); got != "" {
		t.Fatalf("clearly more budget leads: %q", got)
	}
}
