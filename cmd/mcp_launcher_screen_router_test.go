package cmd

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/portmap"
	"andriiklymiuk/corgi/utils/agent/screen"
)

func init() {
	mapRouterPort = func(context.Context, int, time.Duration) (portmap.Mapping, error) {
		return portmap.Mapping{}, portmap.ErrNoRouter
	}
	unmapRouterPort = func(context.Context, portmap.Mapping, int) error { return nil }
}

type fakeRouter struct {
	mu     sync.Mutex
	maps   []int
	unmaps []int
	ext    string
	fail   bool
}

func (f *fakeRouter) install(t *testing.T) {
	t.Helper()
	prevMap, prevUnmap, prevState := mapRouterPort, unmapRouterPort, screenRouter
	screenRouter = &routerState{}
	mapRouterPort = func(_ context.Context, port int, _ time.Duration) (portmap.Mapping, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.maps = append(f.maps, port)
		if f.fail {
			return portmap.Mapping{}, portmap.ErrNoRouter
		}
		return portmap.Mapping{External: net.ParseIP(f.ext), Port: port + 10000, Via: "nat-pmp"}, nil
	}
	unmapRouterPort = func(_ context.Context, m portmap.Mapping, port int) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.unmaps = append(f.unmaps, port)
		return nil
	}
	t.Cleanup(func() { mapRouterPort, unmapRouterPort, screenRouter = prevMap, prevUnmap, prevState })
}

func (f *fakeRouter) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.maps), len(f.unmaps)
}

func TestRouterOpensBothPortsOnceAndClosesThem(t *testing.T) {
	f := &fakeRouter{ext: "203.0.113.20"}
	f.install(t)
	c, ok := screenRouter.Candidate(4100, 4101, time.Now())
	if !ok || c.Kind != "router" || c.Addr != "203.0.113.20:14100/14101" {
		t.Fatalf("%+v %v", c, ok)
	}
	if _, ok := screenRouter.Candidate(4100, 4101, time.Now()); !ok {
		t.Fatal("cached mapping")
	}
	if m, _ := f.counts(); m != 2 {
		t.Fatalf("mapped %d times, want 2 (quic + fast) once", m)
	}
	screenRouter.Release()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, u := f.counts(); u == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release did not close both ports")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRouterBehindCGNATBacksOff(t *testing.T) {
	f := &fakeRouter{ext: "100.70.1.2"}
	f.install(t)
	if _, ok := screenRouter.Candidate(4100, 4101, time.Now()); ok {
		t.Fatal("a carrier-grade NAT address is not reachable")
	}
	if _, ok := screenRouter.Candidate(4100, 4101, time.Now()); ok {
		t.Fatal("still none")
	}
	if m, _ := f.counts(); m != 1 {
		t.Fatalf("asked the router %d times; one try, then an hour of quiet", m)
	}
}

func TestRouterRemapsWhenTheBarRestarts(t *testing.T) {
	f := &fakeRouter{ext: "203.0.113.20"}
	f.install(t)
	_, _ = screenRouter.Candidate(4100, 4101, time.Now())
	c, ok := screenRouter.Candidate(5200, 5201, time.Now())
	if !ok || !strings.HasPrefix(c.Addr, "203.0.113.20:15200") {
		t.Fatalf("%+v", c)
	}
}

func TestScreenStartOffersRouterAndTunnelOnlyWhenAway(t *testing.T) {
	f := &fakeRouter{ext: "203.0.113.20"}
	f.install(t)
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	rig.registerHost(t)
	fk := newFaceKey(t)
	kinds := func() map[string]string {
		code, out := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], true), "")
		if code != 200 {
			t.Fatalf("start: %d %v", code, out)
		}
		got := map[string]string{}
		for _, c := range out["candidates"].([]any) {
			m := c.(map[string]any)
			got[m["kind"].(string)] = m["addr"].(string)
		}
		return got
	}
	away := kinds()
	if away["router"] != "203.0.113.20:14100/14101" || !strings.HasPrefix(away["relay"], screenRelayPath+"?t=") {
		t.Fatalf("away: %v", away)
	}
	_ = runAgentScreen(rig.dir, "router", []string{"off"})
	if got := kinds(); got["router"] != "" || got["relay"] == "" {
		t.Fatalf("router off keeps the tunnel only: %v", got)
	}
	_ = runAgentScreen(rig.dir, "reach", []string{screen.ReachNearby})
	if got := kinds(); got["router"] != "" || got["relay"] != "" {
		t.Fatalf("nearby offers neither: %v", got)
	}
	if err := runAgentScreen(rig.dir, "router", []string{"maybe"}); err == nil {
		t.Fatal("router is on or off")
	}
}

func TestAbsoluteRelay(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"https://mac.example.com/", "/launch/screen/relay?t=a"}: "wss://mac.example.com/launch/screen/relay?t=a",
		{"http://192.168.1.5:8765", "/launch/screen/relay?t=a"}:  "ws://192.168.1.5:8765/launch/screen/relay?t=a",
		{"https://mac.example.com", "192.168.1.5:4100"}:          "192.168.1.5:4100",
		{"ftp://x", "/launch/screen/relay"}:                      "/launch/screen/relay",
	} {
		if got := absoluteRelay(in[0], in[1]); got != want {
			t.Errorf("%v: %s", in, got)
		}
	}
}
