package cmd

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
)

// Laptop B is this process (CORGI_DATA_DIR); laptop A is a bare /pair
// window with role peer. B joins A; then A, as B's paired peer, pulses B.
func TestPeerJoinsThroughAPairWindowAndPulses(t *testing.T) {
	dataB := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dataB)
	dirB, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(dirB, 0o700)

	// A: a pairing window opened for a laptop.
	sessionA, codeA, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	storeA := pairing.StorePath(t.TempDir())
	muxA := http.NewServeMux()
	muxA.Handle("/pair", pairingHandlerWithRole(sessionA, storeA, pairing.RolePeer))
	srvA := httptest.NewServer(muxA)
	defer srvA.Close()

	p, err := peers.Join(context.Background(), dirB, srvA.URL, codeA)
	if err != nil {
		t.Fatal(err)
	}
	if p.Token == "" || p.Key == "" || !strings.HasPrefix(p.Token, pairing.TokenPrefix) {
		t.Fatalf("join: %+v", p)
	}
	devA, _ := pairing.Load(storeA)
	if len(devA.Devices) != 1 || !devA.Devices[0].Peer() || !devA.Devices[0].Encrypted() {
		t.Fatalf("A holds B as an encrypted peer: %+v", devA.Devices)
	}
	stored, _ := peers.Load(peers.Path(dirB))
	if _, ok := stored.Find(p.Name); !ok {
		t.Fatal("B remembers A")
	}

	// A phone's window does not make a peer.
	sessionPhone, codePhone, _ := pairing.NewSession()
	muxP := http.NewServeMux()
	muxP.Handle("/pair", pairingHandler(sessionPhone, pairing.StorePath(t.TempDir())))
	srvP := httptest.NewServer(muxP)
	defer srvP.Close()
	if _, err := peers.Join(context.Background(), dirB, srvP.URL, codePhone); err == nil || !strings.Contains(err.Error(), "not for a laptop") {
		t.Fatalf("a phone window is refused: %v", err)
	}

	// Now A pairs into B the same way, so it can pulse B.
	sessionB, codeB, _ := pairing.NewSession()
	storeB := pairing.StorePath(dirB)
	muxB := http.NewServeMux()
	muxB.Handle("/pair", pairingHandlerWithRole(sessionB, storeB, pairing.RolePeer))
	muxB.Handle("/launch/peers/pulse", launchAuth("", http.HandlerFunc(launchPeersPulseHandler), storeB))
	muxB.Handle("/launch/peers/join", launchAuth("", http.HandlerFunc(launchPeersJoinHandler), storeB))
	muxB.Handle("/launch/board", launchAuth("", http.HandlerFunc(launchBoardHandler), storeB))
	srvB := httptest.NewServer(muxB)
	defer srvB.Close()
	dirA := t.TempDir()
	// A's own key lives in its agent dir; Join reads it from there.
	fromA, err := peers.Join(context.Background(), dirA, srvB.URL, codeB)
	if err != nil {
		t.Fatal(err)
	}
	// B must list A under the name A paired with (its hostname == ours here).
	_ = peers.Update(peers.Path(dirB), func(s *peers.Store) {
		for i := range s.Peers {
			s.Peers[i].Name = peers.Me()
		}
	})
	_ = os.MkdirAll(filepath.Dir(daemon.WatchIdentitiesPath(dirB)), 0o700)
	_ = os.WriteFile(daemon.WatchIdentitiesPath(dirB), []byte(`["linear/api"]`), 0o600)

	var heard peers.Pulse
	client := peers.NewClient(fromA)
	if err := client.Call(context.Background(), "/launch/peers/pulse", peers.Pulse{Name: "A", Watches: []string{"linear/api", "github/x/y"}, Lead: true, At: time.Now().UnixMilli()}, &heard); err != nil {
		t.Fatal(err)
	}
	if heard.Name != peers.Me() || len(heard.Watches) != 1 || heard.Watches[0] != "linear/api" {
		t.Fatalf("B answers with what it watches: %+v", heard)
	}
	after, _ := peers.Load(peers.Path(dirB))
	if len(after.Peers) != 1 || !after.Peers[0].Alive(time.Now()) || !after.Peers[0].Lead || len(after.Peers[0].Watches) != 2 {
		t.Fatalf("B recorded the pulse: %+v", after.Peers)
	}
	if got := peers.Leader(after, "zzz-"+peers.Me(), []string{"linear/api"}, time.Now()); got != peers.Me() {
		t.Fatalf("the peer that leads wins: %q", got)
	}

	// A peer token opens nothing else.
	if err := client.Call(context.Background(), "/launch/board", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("board refused to a peer: %v", err)
	}
	if err := client.Call(context.Background(), "/launch/peers/join", map[string]any{"url": srvA.URL, "code": "X"}, nil); err == nil || !strings.Contains(err.Error(), "pairs back") {
		t.Fatalf("a peer cannot send B to a third laptop: %v", err)
	}
	if k, _ := base64.StdEncoding.DecodeString(fromA.Key); len(k) != 32 {
		t.Fatal("a 32-byte shared key")
	}
}

func TestTunnelNamePerLaptopAndForeignCredentials(t *testing.T) {
	if got := defaultTunnelName("corgi-agent"); got != "corgi-agent" {
		t.Fatalf("saved name wins: %q", got)
	}
	if got := defaultTunnelName(""); !strings.HasPrefix(got, "corgi-") || got == "corgi-" {
		t.Fatalf("a name from the hostname: %q", got)
	}
	list := "ID                                   NAME         CREATED              CONNECTIONS\n" +
		"8f1c2a3b-1111-2222-3333-444455556666 corgi-home   2026-09-01T10:00:00Z 1xfra\n" +
		"9a1c2a3b-1111-2222-3333-444455556666 corgi-agent  2026-08-01T10:00:00Z\n"
	if tunnelIDIn(list, "corgi-agent") != "9a1c2a3b-1111-2222-3333-444455556666" || tunnelIDIn(list, "corgi") != "" {
		t.Fatal("id by exact name")
	}
	old := tunnelCredentialsExist
	defer func() { tunnelCredentialsExist = old }()
	tunnelCredentialsExist = func(string) bool { return false }
	run := func(_ string, args ...string) (string, error) {
		if len(args) > 1 && args[1] == "list" {
			return list, nil
		}
		return "", nil
	}
	err := setupCloudflaredTunnel(run, func(string) error { return nil }, "corgi-agent", "corgi.example.com", false)
	if err == nil || !strings.Contains(err.Error(), "another one") {
		t.Fatalf("a tunnel made elsewhere is refused: %v", err)
	}
	if domainOf("home.corgi.example.com") != "example.com" || domainOf("example.com") != "example.com" {
		t.Fatal("domainOf")
	}
}

func TestCodexNotifyLineIsOursOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	changed, theirs, err := enableCodexNotify(path, "/usr/local/bin/corgi")
	if err != nil || !changed || theirs != "" {
		t.Fatalf("%v %v %q", changed, err, theirs)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), `notify = ["/usr/local/bin/corgi", "agent", "event", "stop", "--agent", "codex", "--notify"]`) {
		t.Fatalf("%s", raw)
	}
	// Idempotent; a moved binary rewrites the line; a table stays below it.
	_ = os.WriteFile(path, append(raw, []byte("[model_providers.x]\nname = \"x\"\n")...), 0o600)
	if changed, _, _ := enableCodexNotify(path, "/usr/local/bin/corgi"); changed {
		t.Fatal("same line twice is no change")
	}
	if changed, _, _ := enableCodexNotify(path, "/opt/corgi"); !changed {
		t.Fatal("a new path rewrites our line")
	}
	raw, _ = os.ReadFile(path)
	if strings.Count(string(raw), "notify =") != 1 || !strings.Contains(string(raw), "[model_providers.x]") {
		t.Fatalf("%s", raw)
	}
	// Someone else's notify is left alone.
	_ = os.WriteFile(path, []byte("notify = [\"say\", \"done\"]\n"), 0o600)
	if changed, theirs, _ := enableCodexNotify(path, "/opt/corgi"); changed || theirs == "" {
		t.Fatal("their notify stays")
	}
	if removed, _ := disableCodexNotify(path); removed {
		t.Fatal("disable never removes theirs")
	}
	_ = os.WriteFile(path, []byte("notify = [\"/opt/corgi\", \"agent\", \"event\", \"stop\", \"--agent\", \"codex\", \"--notify\"]\nfoo = 1\n"), 0o600)
	if removed, _ := disableCodexNotify(path); !removed {
		t.Fatal("disable removes ours")
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != "foo = 1\n" {
		t.Fatalf("%q", raw)
	}
}
