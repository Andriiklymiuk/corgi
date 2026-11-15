package wake

import (
	"errors"
	"net"
	"strings"
)

type Card struct {
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
}

func Local() (Card, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return Card{}, false
	}
	return pick(ifaces, func(i net.Interface) ([]net.Addr, error) { return i.Addrs() })
}

func pick(ifaces []net.Interface, addrsOf func(net.Interface) ([]net.Addr, error)) (Card, bool) {
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) != 6 {
			continue
		}
		if strings.HasPrefix(iface.Name, "utun") || strings.HasPrefix(iface.Name, "awdl") || strings.HasPrefix(iface.Name, "llw") || strings.HasPrefix(iface.Name, "bridge") {
			continue
		}
		addrs, _ := addrsOf(iface)
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := n.IP.To4()
			if v4 == nil || !v4.IsPrivate() || len(n.Mask) != 4 {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = v4[i] | ^n.Mask[i]
			}
			return Card{MAC: iface.HardwareAddr.String(), Broadcast: b.String()}, true
		}
	}
	return Card{}, false
}

func Packet(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, errors.New("not a MAC address: " + mac)
	}
	p := make([]byte, 0, 102)
	p = append(p, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	for i := 0; i < 16; i++ {
		p = append(p, hw...)
	}
	return p, nil
}

func Send(c Card) error {
	p, err := Packet(c.MAC)
	if err != nil {
		return err
	}
	targets := []string{"255.255.255.255"}
	if ip := net.ParseIP(c.Broadcast).To4(); ip != nil {
		targets = append([]string{ip.String()}, targets...)
	}
	var sent int
	var last error
	for _, t := range targets {
		for _, port := range []string{"9", "7"} {
			conn, err := net.Dial("udp4", net.JoinHostPort(t, port))
			if err != nil {
				last = err
				continue
			}
			if _, err := conn.Write(p); err != nil {
				last = err
			} else {
				sent++
			}
			conn.Close()
		}
	}
	if sent == 0 {
		return last
	}
	return nil
}
