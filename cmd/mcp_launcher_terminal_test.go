package cmd

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/term"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

type termRig struct {
	dir  string
	srv  *httptest.Server
	peer peers.Peer
	key  []byte
	hash string
}

func newTermRig(t *testing.T, role string) *termRig {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir := filepath.Join(data, "agent")
	server, err := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	device, _ := ecdh.X25519().GenerateKey(rand.Reader)
	shared, err := pairing.SharedKeyOnDevice(device, server.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := pairing.NewDeviceToken()
	store := &pairing.Store{Version: 1, Devices: []pairing.Device{{Name: "office-mac", TokenHash: pairing.HashToken(tok), CreatedAt: time.Now(), PubKey: pairing.PublicKeyString(device.PublicKey()), Role: role}}}
	if err := pairing.Save(pairing.StorePath(dir), store); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mountLaunchRoutes(mux, "", pairing.StorePath(dir))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &termRig{dir: dir, srv: srv, key: shared, hash: pairing.HashToken(tok),
		peer: peers.Peer{Name: "build-server", URL: srv.URL, Token: tok, Key: base64.StdEncoding.EncodeToString(shared)}}
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestPeerOpensAShellEndToEnd(t *testing.T) {
	rig := newTermRig(t, pairing.RolePeer)
	if _, _, _, err := dialPeerTerminal(rig.peer, rig.key, 80, 24); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("terminals are off by default: %v", err)
	}
	if err := runAgentTerm(rig.dir, "on", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := dialPeerTerminal(rig.peer, rig.key, 80, 24); err == nil || !strings.Contains(err.Error(), "may not") {
		t.Fatalf("a peer needs an allow: %v", err)
	}
	if err := runAgentTerm(rig.dir, "allow", []string{"office"}); err != nil {
		t.Fatal(err)
	}
	conn, box, machine, err := dialPeerTerminal(rig.peer, rig.key, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if machine == "" {
		t.Fatal("the machine names itself")
	}
	in, typer := io.Pipe()
	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		code, _ := term.Attach(conn, box, in, out, make(chan [2]uint16))
		done <- code
	}()
	_, _ = io.WriteString(typer, "echo corgi-$((6*7))\n")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "corgi-42") {
		if time.Now().After(deadline) {
			t.Fatalf("no output: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = io.WriteString(typer, "exit\n")
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("never exited")
	}
	typer.Close()
	var recent []screen.Session
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if recent, _ = screen.Recent(rig.dir, 5); len(recent) >= 2 {
			break
		}
	}
	if len(recent) < 2 || recent[0].Role != "terminal" || recent[0].Event != "end" {
		t.Fatalf("sessions logged: %+v", recent)
	}
	if err := runAgentTerm(rig.dir, "status", nil); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalWebSocketRefusals(t *testing.T) {
	rig := newTermRig(t, pairing.RolePeer)
	_ = runAgentTerm(rig.dir, "on", nil)
	_ = runAgentTerm(rig.dir, "allow", []string{"office-mac"})
	ws := "ws" + strings.TrimPrefix(rig.srv.URL, "http") + terminalWSPath
	if _, err := wsock.Dial(ws+"?ticket=made-up", http.Header{"Authorization": {"Bearer " + rig.peer.Token}}, 5*time.Second); err == nil {
		t.Fatal("a made-up ticket opened a shell")
	}
	tk, _ := screen.MintTicket(rig.dir, rig.hash, "office-mac", time.Now())
	if _, err := wsock.Dial(ws+"?ticket="+tk, http.Header{"Authorization": {"Bearer cdev_wrong"}}, 5*time.Second); err == nil {
		t.Fatal("a stranger used a real ticket")
	}
	c, err := wsock.Dial(ws+"?ticket="+tk, http.Header{"Authorization": {"Bearer " + rig.peer.Token}}, 5*time.Second)
	if err != nil {
		t.Fatalf("the real ticket was spent by the refused stranger: %v", err)
	}
	c.Close()
	if _, err := wsock.Dial(ws+"?ticket="+tk, http.Header{"Authorization": {"Bearer " + rig.peer.Token}}, 5*time.Second); err == nil {
		t.Fatal("a ticket opened two shells")
	}
	_ = runAgentTerm(rig.dir, "deny", []string{"office-mac"})
	tk2, _ := screen.MintTicket(rig.dir, rig.hash, "office-mac", time.Now())
	if _, err := wsock.Dial(ws+"?ticket="+tk2, http.Header{"Authorization": {"Bearer " + rig.peer.Token}}, 5*time.Second); err == nil {
		t.Fatal("deny did not stick")
	}
}

func TestPhoneTerminalNeedsFaceID(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentTerm(rig.dir, "on", nil)
	_, _ = screen.Update(rig.dir, func(s *screen.Settings) error {
		s.Devices[rig.hashes["ipad"]] = &screen.Grant{Name: "iPad mini", Terminal: true}
		return nil
	})
	code, out := rig.call(t, launchTerminalHandler, "ipad", "POST", "/launch/terminal", `{"do":"open","cols":80,"rows":24}`, "")
	if code != 403 {
		t.Fatalf("no Face ID key: %d %v", code, out)
	}
	fk := newFaceKey(t)
	code, out = rig.call(t, launchTerminalHandler, "ipad", "POST", "/launch/terminal", fk.start(t, rig.hashes["ipad"], true), "")
	if code != 200 || out["ticket"] == nil || out["path"] != terminalWSPath {
		t.Fatalf("with Face ID: %d %v", code, out)
	}
	for _, who := range []string{"viewer", "connector", "bar"} {
		if code, _ := rig.call(t, launchTerminalHandler, who, "POST", "/launch/terminal", `{}`, ""); code != 403 {
			t.Errorf("%s got %d", who, code)
		}
	}
	if code, out := rig.call(t, launchTerminalHandler, "ipad", "GET", "/launch/terminal", "", ""); code != 200 || out["allowed"] != true {
		t.Fatalf("status: %d %v", code, out)
	}
}

func TestTerminalCLIWords(t *testing.T) {
	rig := newScreenRig(t)
	for _, bad := range [][]string{{"allow"}, {"allow", "nobody"}, {"deny"}} {
		if err := runAgentTerm(rig.dir, bad[0], bad[1:]); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
	if err := runAgentTerm(rig.dir, "no-such-peer", nil); err == nil {
		t.Fatal("unknown peer accepted")
	}
	if _, err := peerWebSocketURL("ftp://x", "/p", nil); err == nil {
		t.Fatal("ftp peer url accepted")
	}
	if u, _ := peerWebSocketURL("https://a.example.com/", "/launch/terminal/ws", map[string][]string{"ticket": {"t"}}); u != "wss://a.example.com/launch/terminal/ws?ticket=t" {
		t.Fatalf("url %s", u)
	}
	_ = runAgentTerm(rig.dir, "off", nil)
	if sizeArg("x", 80) != 80 || sizeArg("120", 80) != 120 || sizeArg("5000", 24) != 24 {
		t.Fatal("sizes")
	}
}
