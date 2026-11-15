package portmap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Mapping struct {
	External net.IP
	Port     int
	Via      string
	control  string
	service  string
}

var ErrNoRouter = errors.New("the router does not open ports (no NAT-PMP or UPnP)")

var (
	natpmpPort   = 5351
	gatewayFunc  = Gateway
	locationFunc = ssdpLocation
)

func Gateway() (net.IP, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("route", "-n", "get", "default").Output()
		if err != nil {
			return nil, err
		}
		return parseRouteGet(out)
	case "linux":
		raw, err := os.ReadFile("/proc/net/route")
		if err != nil {
			return nil, err
		}
		return parseProcRoute(raw)
	}
	return nil, errors.New("no gateway lookup on " + runtime.GOOS)
}

func parseRouteGet(out []byte) (net.IP, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "gateway:"); ok {
			if ip := net.ParseIP(strings.TrimSpace(v)).To4(); ip != nil {
				return ip, nil
			}
		}
	}
	return nil, errors.New("no default gateway")
}

func parseProcRoute(raw []byte) (net.IP, error) {
	for _, line := range strings.Split(string(raw), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil || v == 0 {
			continue
		}
		ip := make(net.IP, 4)
		binary.LittleEndian.PutUint32(ip, uint32(v))
		return ip, nil
	}
	return nil, errors.New("no default gateway")
}

func Map(ctx context.Context, port int, lifetime time.Duration) (Mapping, error) {
	gw, err := gatewayFunc()
	if err != nil {
		return Mapping{}, err
	}
	if m, err := natpmpMap(ctx, gw, port, port, lifetime); err == nil {
		return m, nil
	}
	return upnpMap(ctx, gw, port, lifetime)
}

func Unmap(ctx context.Context, m Mapping, port int) error {
	if m.Via == "upnp" {
		return upnpDelete(ctx, m)
	}
	gw, err := gatewayFunc()
	if err != nil {
		return err
	}
	_, err = natpmpMap(ctx, gw, port, 0, 0)
	return err
}

func Usable(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsPrivate() || v4.IsLoopback() || v4.IsUnspecified() || v4.IsLinkLocalUnicast() {
		return false
	}
	cgnat := &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}
	return !cgnat.Contains(v4)
}

func natpmpCall(ctx context.Context, gw net.IP, req []byte, want byte, size int) ([]byte, error) {
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: gw, Port: natpmpPort})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	buf := make([]byte, 32)
	wait := 250 * time.Millisecond
	for try := 0; try < 2; try++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		n, err := conn.Read(buf)
		if err != nil {
			wait *= 2
			continue
		}
		if n < size || buf[0] != 0 || buf[1] != want {
			continue
		}
		if code := binary.BigEndian.Uint16(buf[2:4]); code != 0 {
			return nil, fmt.Errorf("NAT-PMP said no (code %d)", code)
		}
		return buf[:n], nil
	}
	return nil, errors.New("no NAT-PMP answer")
}

func natpmpMap(ctx context.Context, gw net.IP, internal, external int, lifetime time.Duration) (Mapping, error) {
	req := make([]byte, 12)
	req[1] = 1
	binary.BigEndian.PutUint16(req[4:], uint16(internal))
	binary.BigEndian.PutUint16(req[6:], uint16(external))
	binary.BigEndian.PutUint32(req[8:], uint32(lifetime/time.Second))
	resp, err := natpmpCall(ctx, gw, req, 129, 16)
	if err != nil {
		return Mapping{}, err
	}
	if lifetime == 0 {
		return Mapping{Via: "nat-pmp"}, nil
	}
	ipResp, err := natpmpCall(ctx, gw, []byte{0, 0}, 128, 12)
	if err != nil {
		return Mapping{}, err
	}
	return Mapping{External: net.IP(append([]byte(nil), ipResp[8:12]...)), Port: int(binary.BigEndian.Uint16(resp[10:12])), Via: "nat-pmp"}, nil
}

func ssdpLocation(ctx context.Context) (string, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	for _, st := range []string{"urn:schemas-upnp-org:device:InternetGatewayDevice:1", "urn:schemas-upnp-org:device:InternetGatewayDevice:2"} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: " + st + "\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\n\r\n"
		_, _ = conn.WriteTo([]byte(msg), dst)
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return "", errors.New("no UPnP router answered")
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:n])), nil)
		if err != nil {
			continue
		}
		if loc := resp.Header.Get("Location"); loc != "" {
			return loc, nil
		}
	}
}

type upnpDevice struct {
	URLBase string        `xml:"URLBase"`
	Device  upnpDeviceTag `xml:"device"`
}

type upnpDeviceTag struct {
	Services []upnpService   `xml:"serviceList>service"`
	Devices  []upnpDeviceTag `xml:"deviceList>device"`
}

type upnpService struct {
	Type    string `xml:"serviceType"`
	Control string `xml:"controlURL"`
}

func findWAN(d upnpDeviceTag) (upnpService, bool) {
	for _, s := range d.Services {
		if strings.Contains(s.Type, "WANIPConnection") || strings.Contains(s.Type, "WANPPPConnection") {
			return s, true
		}
	}
	for _, sub := range d.Devices {
		if s, ok := findWAN(sub); ok {
			return s, true
		}
	}
	return upnpService{}, false
}

var upnpClient = &http.Client{Timeout: 3 * time.Second}

func wanControl(ctx context.Context) (string, string, error) {
	loc, err := locationFunc(ctx)
	if err != nil {
		return "", "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	res, err := upnpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	var dev upnpDevice
	if err := xml.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&dev); err != nil {
		return "", "", err
	}
	svc, ok := findWAN(dev.Device)
	if !ok {
		return "", "", ErrNoRouter
	}
	base, _ := url.Parse(loc)
	if dev.URLBase != "" {
		if b, err := url.Parse(dev.URLBase); err == nil {
			base = b
		}
	}
	ctrl, err := base.Parse(svc.Control)
	if err != nil {
		return "", "", err
	}
	return ctrl.String(), svc.Type, nil
}

func soap(ctx context.Context, control, service, action, args string) (string, error) {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` +
		action + ` xmlns:u="` + service + `">` + args + `</u:` + action + `></s:Body></s:Envelope>`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, control, strings.NewReader(body))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+service+`#`+action+`"`)
	res, err := upnpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("UPnP %s said %d", action, res.StatusCode)
	}
	return string(raw), nil
}

func xmlText(doc, tag string) string {
	_, after, ok := strings.Cut(doc, "<"+tag+">")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(after, "</"+tag+">")
	return strings.TrimSpace(v)
}

func localIPFor(gw net.IP) string {
	c, err := net.Dial("udp4", net.JoinHostPort(gw.String(), "1900"))
	if err != nil {
		return ""
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

func upnpMap(ctx context.Context, gw net.IP, port int, lifetime time.Duration) (Mapping, error) {
	control, service, err := wanControl(ctx)
	if err != nil {
		return Mapping{}, err
	}
	ipDoc, err := soap(ctx, control, service, "GetExternalIPAddress", "")
	if err != nil {
		return Mapping{}, err
	}
	ext := net.ParseIP(xmlText(ipDoc, "NewExternalIPAddress"))
	p := strconv.Itoa(port)
	args := "<NewRemoteHost></NewRemoteHost><NewExternalPort>" + p + "</NewExternalPort><NewProtocol>UDP</NewProtocol><NewInternalPort>" + p +
		"</NewInternalPort><NewInternalClient>" + localIPFor(gw) + "</NewInternalClient><NewEnabled>1</NewEnabled><NewPortMappingDescription>corgi screen</NewPortMappingDescription><NewLeaseDuration>" +
		strconv.Itoa(int(lifetime/time.Second)) + "</NewLeaseDuration>"
	if _, err := soap(ctx, control, service, "AddPortMapping", args); err != nil {
		return Mapping{}, err
	}
	return Mapping{External: ext, Port: port, Via: "upnp", control: control, service: service}, nil
}

func upnpDelete(ctx context.Context, m Mapping) error {
	if m.control == "" {
		control, service, err := wanControl(ctx)
		if err != nil {
			return err
		}
		m.control, m.service = control, service
	}
	_, err := soap(ctx, m.control, m.service, "DeletePortMapping", "<NewRemoteHost></NewRemoteHost><NewExternalPort>"+strconv.Itoa(m.Port)+"</NewExternalPort><NewProtocol>UDP</NewProtocol>")
	return err
}
