package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/term"
	"andriiklymiuk/corgi/utils/agent/wake"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

func (rig *termRig) withSelfPeer(t *testing.T, mac string) {
	t.Helper()
	p := rig.peer
	p.MAC, p.Broadcast = mac, "192.168.1.255"
	if err := peers.Save(peers.Path(rig.dir), &peers.Store{Peers: []peers.Peer{p}}); err != nil {
		t.Fatal(err)
	}
}

func (rig *termRig) barToken(t *testing.T) string {
	t.Helper()
	tok, _ := pairing.NewDeviceToken()
	store, _ := pairing.Load(pairing.StorePath(rig.dir))
	store.Devices = append(store.Devices, pairing.Device{Name: "Corgi Agents for Mac (Test)", TokenHash: pairing.HashToken(tok), CreatedAt: time.Now()})
	if err := pairing.Save(pairing.StorePath(rig.dir), store); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestBarOpensAPeersScreenWithTouchID(t *testing.T) {
	rig := newTermRig(t, pairing.RolePeer)
	rig.withSelfPeer(t, "")
	_ = runAgentScreen(rig.dir, "on", nil)
	_ = screen.RegisterHost(rig.dir, screen.Host{Port: 4500, FastPort: 4501, Fingerprint: strings.Repeat("ab", 32)}, time.Now())
	ch, err := peerScreenChallenge(rig.dir, "build-server")
	if err != nil || ch["device"] != rig.hash {
		t.Fatalf("challenge: %v %v", ch, err)
	}
	fk := newFaceKey(t)
	if _, err := peerScreenOpen(rig.dir, peerScreenIn{Peer: "build-server", Width: 1440, Height: 900, Scale: 200, FPS: 60}); err == nil {
		t.Fatal("no Touch ID proof, no ticket")
	}
	signed := func() peerScreenIn {
		in := peerScreenIn{Peer: "build-server", Width: 1440, Height: 900, Scale: 200, FPS: 60, Key: fk.pub}
		body := fk.start(t, rig.hash, true)
		var m map[string]any
		_ = json.Unmarshal([]byte(body), &m)
		in.TS, in.Sig = int64(m["ts"].(float64)), m["sig"].(string)
		return in
	}
	out, err := peerScreenOpen(rig.dir, signed())
	if err != nil || out["ticket"] == nil || out["role"] != screen.RoleAsk || out["peer"] != "build-server" {
		t.Fatalf("open: %v %v", out, err)
	}
	if _, err := peerScreenOpen(rig.dir, peerScreenIn{Peer: "nobody"}); err == nil {
		t.Fatal("unknown peer")
	}
}

func TestBarTerminalRelayToAPeer(t *testing.T) {
	rig := newTermRig(t, pairing.RolePeer)
	rig.withSelfPeer(t, "")
	_ = runAgentTerm(rig.dir, "on", nil)
	_ = runAgentTerm(rig.dir, "allow", []string{"office-mac"})
	bar := rig.barToken(t)
	ws := "ws" + strings.TrimPrefix(rig.srv.URL, "http") + "/launch/terminal/peer?peer=build-server&cols=90&rows=30"
	if _, err := wsock.Dial(ws, http.Header{"Authorization": {"Bearer " + rig.peer.Token}}, 5*time.Second); err == nil {
		t.Fatal("a peer token used the bar's relay")
	}
	conn, err := wsock.Dial(ws, http.Header{"Authorization": {"Bearer " + bar}}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(wsock.OpBinary, term.Data([]byte("echo relay-$((5*5))\n"))); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(seen.String(), "relay-25") {
		if time.Now().After(deadline) {
			t.Fatalf("no output: %q", seen.String())
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, m, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (seen %q)", err, seen.String())
		}
		if len(m) > 0 && m[0] == term.KindData {
			seen.Write(m[1:])
		}
	}
	_ = conn.WriteMessage(wsock.OpBinary, term.Data([]byte("exit 4\n")))
	for {
		_, m, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no exit frame: %v", err)
		}
		if code, ok := term.ParseExit(m); ok {
			if code != 4 {
				t.Fatalf("exit %d", code)
			}
			return
		}
	}
}

func TestWakeAPeer(t *testing.T) {
	rig := newTermRig(t, pairing.RolePeer)
	rig.withSelfPeer(t, "")
	var sent []wake.Card
	prev := sendWake
	sendWake = func(c wake.Card) error { sent = append(sent, c); return nil }
	t.Cleanup(func() { sendWake = prev })
	if err := wakePeer(rig.dir, "build-server"); err == nil || !strings.Contains(err.Error(), "2.51") {
		t.Fatalf("no MAC yet: %v", err)
	}
	rig.withSelfPeer(t, "aa:bb:cc:dd:ee:ff")
	if err := wakePeer(rig.dir, "build-server"); err != nil || len(sent) != 1 || sent[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("wake: %v %v", err, sent)
	}
	req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+"/launch/peers/wake", strings.NewReader(`{}`))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wake without a token: %d", res.StatusCode)
	}
	if code := exitsWith(t, func() { agentWakeCmd.Run(agentWakeCmd, []string{"build-server"}) }); code != -1 || len(sent) != 2 {
		t.Fatalf("cli wake: %d %v", code, sent)
	}
	if code := exitsWith(t, func() { agentWakeCmd.Run(agentWakeCmd, []string{"nobody"}) }); code != 1 {
		t.Fatalf("unknown peer exits 1, got %d", code)
	}
}

func TestPeerAbsorbsTheWakeCard(t *testing.T) {
	var p peers.Peer
	p.Absorb(peers.Pulse{MAC: "11:22:33:44:55:66", Broadcast: "10.0.0.255"}, time.Now())
	if p.MAC != "11:22:33:44:55:66" || p.Public()["wakeable"] != true {
		t.Fatalf("%+v", p)
	}
	p.Absorb(peers.Pulse{}, time.Now())
	if p.MAC == "" {
		t.Fatal("an older pulse keeps the card")
	}
	_ = io.Discard
}
