package portmap

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRouteParsers(t *testing.T) {
	ip, err := parseRouteGet([]byte("   route to: default\ndestination: default\n    gateway: 192.168.0.1\n  interface: en0\n"))
	if err != nil || ip.String() != "192.168.0.1" {
		t.Fatalf("%v %v", ip, err)
	}
	if _, err := parseRouteGet([]byte("interface: utun4\n")); err == nil {
		t.Fatal("no gateway line")
	}
	raw := "Iface\tDestination\tGateway\tFlags\neth0\t00000000\t0101A8C0\t0003\n"
	ip, err = parseProcRoute([]byte(raw))
	if err != nil || ip.String() != "192.168.1.1" {
		t.Fatalf("%v %v", ip, err)
	}
}

func TestUsable(t *testing.T) {
	for ip, want := range map[string]bool{"203.0.113.9": true, "192.168.1.5": false, "100.72.1.1": false, "10.0.0.1": false, "0.0.0.0": false, "2001:db8::1": false} {
		if Usable(net.ParseIP(ip)) != want {
			t.Errorf("%s want %v", ip, want)
		}
	}
}

func fakeNATPMP(t *testing.T, refuse bool) func() []uint32 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	prev, prevGW := natpmpPort, gatewayFunc
	natpmpPort = conn.LocalAddr().(*net.UDPAddr).Port
	gatewayFunc = func() (net.IP, error) { return net.IPv4(127, 0, 0, 1), nil }
	t.Cleanup(func() { natpmpPort, gatewayFunc = prev, prevGW })
	var mu sync.Mutex
	lifetimes := &[]uint32{}
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if refuse {
				continue
			}
			switch {
			case n == 2 && buf[1] == 0:
				_, _ = conn.WriteToUDP([]byte{0, 128, 0, 0, 0, 0, 0, 1, 203, 0, 113, 7}, from)
			case n == 12 && buf[1] == 1:
				mu.Lock()
				*lifetimes = append(*lifetimes, binary.BigEndian.Uint32(buf[8:12]))
				mu.Unlock()
				resp := make([]byte, 16)
				resp[1] = 129
				copy(resp[8:10], buf[4:6])
				copy(resp[10:12], buf[4:6])
				copy(resp[12:16], buf[8:12])
				_, _ = conn.WriteToUDP(resp, from)
			}
		}
	}()
	return func() []uint32 {
		mu.Lock()
		defer mu.Unlock()
		return append([]uint32(nil), *lifetimes...)
	}
}

func TestNATPMPMapsAndUnmaps(t *testing.T) {
	seen := fakeNATPMP(t, false)
	m, err := Map(context.Background(), 51663, 30*time.Minute)
	if err != nil || m.Via != "nat-pmp" || m.External.String() != "203.0.113.7" || m.Port != 51663 {
		t.Fatalf("%+v %v", m, err)
	}
	if err := Unmap(context.Background(), m, 51663); err != nil {
		t.Fatal(err)
	}
	if l := seen(); len(l) != 2 || l[0] != 1800 || l[1] != 0 {
		t.Fatalf("lifetimes %v", l)
	}
}

func TestUPnPWhenNATPMPIsSilent(t *testing.T) {
	fakeNATPMP(t, true)
	var mu sync.Mutex
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `<?xml version="1.0"?><root><device><deviceList><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl/IPConn</controlURL></service></serviceList></device></deviceList></device></root>`)
			return
		}
		action := r.Header.Get("SOAPAction")
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		actions = append(actions, action[strings.Index(action, "#")+1:len(action)-1])
		mu.Unlock()
		if strings.Contains(action, "GetExternalIPAddress") {
			_, _ = io.WriteString(w, `<s:Envelope><s:Body><u:R><NewExternalIPAddress>198.51.100.4</NewExternalIPAddress></u:R></s:Body></s:Envelope>`)
			return
		}
		if strings.Contains(action, "AddPortMapping") && !strings.Contains(string(body), "<NewExternalPort>51663</NewExternalPort>") {
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	prev := locationFunc
	locationFunc = func(context.Context) (string, error) { return srv.URL + "/rootDesc.xml", nil }
	defer func() { locationFunc = prev }()
	m, err := Map(context.Background(), 51663, time.Hour)
	if err != nil || m.Via != "upnp" || m.External.String() != "198.51.100.4" {
		t.Fatalf("%+v %v", m, err)
	}
	if err := Unmap(context.Background(), m, 51663); err != nil {
		t.Fatal(err)
	}
	if strings.Join(actions, ",") != "GetExternalIPAddress,AddPortMapping,DeletePortMapping" {
		t.Fatalf("%v", actions)
	}
}

func TestNoRouterIsAnError(t *testing.T) {
	fakeNATPMP(t, true)
	prev := locationFunc
	locationFunc = func(context.Context) (string, error) { return "", ErrNoRouter }
	defer func() { locationFunc = prev }()
	if _, err := Map(context.Background(), 4000, time.Minute); err == nil {
		t.Fatal("silent router mapped")
	}
}
