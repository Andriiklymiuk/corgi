package cmd

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

func echoHost(t *testing.T, dir string) {
	t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := udp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteToUDP(append([]byte("echo:"), buf[:n]...), from)
		}
	}()
	port := udp.LocalAddr().(*net.UDPAddr).Port
	if err := screen.RegisterHost(dir, screen.Host{Port: port, FastPort: port + 1, Fingerprint: strings.Repeat("ab", 32)}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTheTunnelCarriesDatagramsToTheBar(t *testing.T) {
	rig := newTermRig(t, "")
	_ = runAgentScreen(rig.dir, "on", nil)
	echoHost(t, rig.dir)
	relay, ok := mintScreenRelay(rig.dir, rig.hash, "iPhone", time.Now())
	if !ok {
		t.Fatal("no relay ticket")
	}
	url := absoluteRelay(rig.srv.URL, relay.Addr)
	conn, err := wsock.Dial(url, http.Header{"Accept-Encoding": {"gzip"}}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 50; i++ {
		msg := []byte("quic-packet-" + strings.Repeat("x", i*20))
		if err := conn.WriteMessage(wsock.OpBinary, msg); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, back, err := conn.ReadMessage()
		if err != nil || string(back) != "echo:"+string(msg) {
			t.Fatalf("%d: %q %v", i, back, err)
		}
	}
	if _, err := wsock.Dial(url, nil, 3*time.Second); err == nil {
		t.Fatal("a relay ticket opens one tunnel")
	}
}

func TestTheTunnelRefusesWrongTicketsAndNearby(t *testing.T) {
	rig := newTermRig(t, "")
	_ = runAgentScreen(rig.dir, "on", nil)
	echoHost(t, rig.dir)
	plain, _ := screen.MintTicket(rig.dir, rig.hash, "iPhone", time.Now())
	if _, err := wsock.Dial(absoluteRelay(rig.srv.URL, screenRelayPath+"?t="+plain), nil, 3*time.Second); err == nil {
		t.Fatal("a screen ticket is not a relay ticket")
	}
	if _, err := wsock.Dial(absoluteRelay(rig.srv.URL, screenRelayPath+"?t=made-up"), nil, 3*time.Second); err == nil {
		t.Fatal("made-up ticket")
	}
	relay, _ := mintScreenRelay(rig.dir, rig.hash, "iPhone", time.Now())
	_ = runAgentScreen(rig.dir, "reach", []string{screen.ReachNearby})
	if _, err := wsock.Dial(absoluteRelay(rig.srv.URL, relay.Addr), nil, 3*time.Second); err == nil {
		t.Fatal("nearby only refuses the tunnel")
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 900: "900 B", 2048: "2.0 KB", 5 << 20: "5.0 MB", 3 << 30: "3.0 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("%d: %s", n, got)
		}
	}
}
