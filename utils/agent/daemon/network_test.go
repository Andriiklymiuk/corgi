package daemon

import (
	"strings"
	"testing"
	"time"
)

func TestTheHopperJoinsAfterAMinuteAndAHalfOffline(t *testing.T) {
	h := &hopper{ssid: "Andrii iPhone"}
	start := time.Now()
	if s := h.tick(start, false, "Hotel", 0); s.Join || s.Say != "" {
		t.Fatalf("the first miss is not an outage: %+v", s)
	}
	if s := h.tick(start.Add(time.Minute), false, "Hotel", 0); s.Join {
		t.Fatal("a minute is too soon - Wi-Fi hiccups")
	}
	if s := h.tick(start.Add(2*time.Minute), false, "", 0); !s.Join {
		t.Fatal("two minutes offline joins the hotspot")
	}
	if s := h.tick(start.Add(2*time.Minute+10*time.Second), false, "", 0); s.Join {
		t.Fatal("the second try waits for the backoff")
	}
	if s := h.tick(start.Add(3*time.Minute), false, "", 0); !s.Join {
		t.Fatal("30 seconds later it tries again")
	}
	s := h.tick(start.Add(3*time.Minute+20*time.Second), true, "Andrii iPhone", 1000)
	if !strings.Contains(s.Say, "offline for 3 minutes") || !strings.Contains(s.Say, "hopped onto the hotspot Andrii iPhone") {
		t.Fatalf("the recovery names the gap and the hotspot: %q", s.Say)
	}
	if h.attempts != 0 || !h.onHotspot {
		t.Fatalf("reset after recovery: attempts=%d onHotspot=%v", h.attempts, h.onHotspot)
	}
}

func TestTheHopperWithNoHotspotOnlySaysTheGap(t *testing.T) {
	h := &hopper{}
	start := time.Now()
	h.tick(start, false, "", 0)
	if s := h.tick(start.Add(5*time.Minute), false, "", 0); s.Join {
		t.Fatal("nothing to join")
	}
	if s := h.tick(start.Add(6*time.Minute), true, "Hotel", 0); s.Say != "back online after 6 minutes" {
		t.Fatalf("got %q", s.Say)
	}
}

func TestTheHopperHoldsRunsPastTheDataBudget(t *testing.T) {
	h := &hopper{ssid: "phone", budget: 1 << 30}
	now := time.Now()
	if s := h.tick(now, true, "phone", 5<<30); s.Hold || s.Say != "" {
		t.Fatalf("joining the hotspot starts the count at zero: %+v", s)
	}
	if s := h.tick(now.Add(time.Minute), true, "phone", 5<<30+500<<20); s.Hold {
		t.Fatal("500 MB is under the budget")
	}
	s := h.tick(now.Add(2*time.Minute), true, "phone", 5<<30+1100<<20)
	if !s.Hold || !strings.Contains(s.Say, "has used 1.1 GB of its 1.0 GB budget") {
		t.Fatalf("past the budget holds and says so: %+v", s)
	}
	if s := h.tick(now.Add(3*time.Minute), true, "phone", 5<<30+1200<<20); !s.Hold || s.Say != "" {
		t.Fatalf("still over, said once: %+v", s)
	}
	s = h.tick(now.Add(4*time.Minute), true, "Hotel", 5<<30+1200<<20)
	if s.Hold || !strings.Contains(s.Say, "back on Hotel after 4 minutes on the hotspot (1.2 GB used)") {
		t.Fatalf("Wi-Fi back lifts the hold: %+v", s)
	}
}

func TestAnInstantHotspotMatchesByName(t *testing.T) {
	if !onHotspotName("Andrii's iPhone", "andrii's iphone") || !onHotspotName("iPhone (2)", "iPhone") {
		t.Fatal("case and suffixes do not matter")
	}
	if onHotspotName("Hotel", "iPhone") || onHotspotName("", "iPhone") || onHotspotName("iPhone", "") {
		t.Fatal("not the hotspot")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]uint64{"2GB": 2 << 30, "500MB": 500 << 20, "1.5 gb": 3 << 29, "0": 0, "off": 0, "": 0} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %d, %v", in, got, err)
		}
	}
	if _, err := ParseSize("lots"); err == nil {
		t.Fatal("lots is not a size")
	}
}

func TestMacOSNetworkParsers(t *testing.T) {
	ports := "Hardware Port: Ethernet\nDevice: en5\n\nHardware Port: Wi-Fi\nDevice: en0\nEthernet Address: aa\n"
	if got := wifiInterfaceFrom(ports); got != "en0" {
		t.Fatalf("got %q", got)
	}
	if got := ssidFrom("Current Wi-Fi Network: Hotel Lobby\n"); got != "Hotel Lobby" {
		t.Fatalf("got %q", got)
	}
	if got := ssidFrom("You are not associated with an AirPort network.\n"); got != "" {
		t.Fatalf("got %q", got)
	}
	netstat := "Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll\n" +
		"en0        1500  <Link#14>   22:77:7d:84:0b:a8   909674     0  939391679   659229     0  516697371     0\n" +
		"en0        1500  macbook-pro fe80:e::d9:b673:8   909674     -  939391679   659229     -  516697371     -\n"
	if got := bytesFrom(netstat); got != 939391679+516697371 {
		t.Fatalf("got %d", got)
	}
	if sizeOf(1536<<20) != "1.5 GB" || sizeOf(20<<20) != "20 MB" {
		t.Fatal(sizeOf(1536<<20), sizeOf(20<<20))
	}
}
