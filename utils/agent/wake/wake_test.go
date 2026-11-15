package wake

import (
	"bytes"
	"net"
	"testing"
)

func TestPacket(t *testing.T) {
	p, err := Packet("aa:bb:cc:dd:ee:ff")
	if err != nil || len(p) != 102 || !bytes.Equal(p[:6], bytes.Repeat([]byte{0xff}, 6)) || !bytes.Equal(p[96:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}) {
		t.Fatalf("%x %v", p, err)
	}
	if _, err := Packet("nope"); err == nil {
		t.Fatal("bad mac")
	}
}

func TestPickTheLANCard(t *testing.T) {
	hw, _ := net.ParseMAC("11:22:33:44:55:66")
	ifaces := []net.Interface{
		{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "utun3", Flags: net.FlagUp, HardwareAddr: hw},
		{Name: "en0", Flags: net.FlagUp, HardwareAddr: hw},
	}
	addrs := map[string][]net.Addr{
		"utun3": {&net.IPNet{IP: net.ParseIP("100.64.1.2"), Mask: net.CIDRMask(32, 32)}},
		"en0":   {&net.IPNet{IP: net.ParseIP("192.168.1.23").To4(), Mask: net.CIDRMask(24, 32)}},
	}
	c, ok := pick(ifaces, func(i net.Interface) ([]net.Addr, error) { return addrs[i.Name], nil })
	if !ok || c.MAC != "11:22:33:44:55:66" || c.Broadcast != "192.168.1.255" {
		t.Fatalf("%+v %v", c, ok)
	}
	if _, ok := pick(ifaces[:2], func(i net.Interface) ([]net.Addr, error) { return addrs[i.Name], nil }); ok {
		t.Fatal("no LAN card should mean none")
	}
}

func TestSendRejectsGarbage(t *testing.T) {
	if Send(Card{MAC: "x"}) == nil {
		t.Fatal("garbage mac sent")
	}
}
