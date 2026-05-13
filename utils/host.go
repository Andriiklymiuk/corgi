package utils

import (
	"fmt"
	"net"
	"strings"
)

// HostOverride is the value of `corgi run --host`. Rewrites "localhost"
// in generated service URLs, and in the rest of the .env when
// LocalhostNameInEnv isn't set.
var HostOverride string

// Interfaces a phone can't actually reach: Docker bridges, VPN tunnels,
// AirDrop, loopback.
var virtualIfacePrefixes = []string{
	"utun", "bridge", "vmnet", "vmenet", "docker", "veth",
	"awdl", "llw", "tun", "tap", "anpi", "ap", "lo",
}

// Overridable so tests can swap in fake interfaces.
var getInterfaces = func() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	return ifaces
}

func DetectHostIP() (string, error) {
	return PickHostIPFromInterfaces(getInterfaces())
}

// Prefer the usual Wi-Fi/Ethernet names, then fall back to anything real.
func PickHostIPFromInterfaces(ifaces []net.Interface) (string, error) {
	if len(ifaces) == 0 {
		return "", fmt.Errorf("no network interfaces available")
	}

	ifaceByName := map[string]net.Interface{}
	for _, iface := range ifaces {
		ifaceByName[iface.Name] = iface
	}

	for _, name := range []string{"en0", "en1", "eth0", "wlan0"} {
		if iface, ok := ifaceByName[name]; ok {
			if ip := firstIPv4(iface); ip != "" {
				return ip, nil
			}
		}
	}
	for _, iface := range ifaces {
		if isVirtualIface(iface.Name) {
			continue
		}
		if ip := firstIPv4(iface); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no non-loopback IPv4 address found on any interface")
}

func isVirtualIface(name string) bool {
	for _, prefix := range virtualIfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func firstIPv4(iface net.Interface) string {
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() {
			continue
		}
		return ip.String()
	}
	return ""
}

func ServiceHost() string {
	if HostOverride != "" {
		return HostOverride
	}
	return "localhost"
}
