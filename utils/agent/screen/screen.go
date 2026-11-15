package screen

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RoleOff     = "off"
	RoleWatch   = "watch"
	RoleControl = "control"
	RoleAsk     = "ask"

	ReachNearby = "nearby"
	ReachAway   = "away"

	TicketTTL   = 60 * time.Second
	HostStale   = 90 * time.Second
	SignWindow  = 2 * time.Minute
	maxTickets  = 32
	signPrefix  = "corgi-screen-v1"
	settingsDir = "screen"
)

type Grant struct {
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	Terminal bool      `json:"terminal,omitempty"`
	Key      string    `json:"key,omitempty"`
	Since    time.Time `json:"since"`
	Last     time.Time `json:"last,omitempty"`
}

type Settings struct {
	Enabled     bool              `json:"enabled"`
	Reach       string            `json:"reach,omitempty"`
	AskOnMac    bool              `json:"askOnMac,omitempty"`
	WhileLocked bool              `json:"whileLocked,omitempty"`
	Clipboard   bool              `json:"clipboard,omitempty"`
	NoFiles     bool              `json:"noFiles,omitempty"`
	Curtain     bool              `json:"curtain,omitempty"`
	IdleStop    string            `json:"idleStop,omitempty"`
	Hide        []string          `json:"hide,omitempty"`
	Devices     map[string]*Grant `json:"devices,omitempty"`
	Terminal    bool              `json:"terminal,omitempty"`
}

func (s *Settings) TerminalFor(device string) bool {
	if !s.Terminal {
		return false
	}
	g, ok := s.Devices[device]
	return ok && g.Terminal
}

var DefaultHide = []string{
	"com.1password.1password",
	"com.agilebits.onepassword7",
	"com.bitwarden.desktop",
	"com.apple.Passwords",
	"com.apple.MobileSMS",
	"com.apple.mail",
}

func (s *Settings) normalize() {
	if s.Reach != ReachNearby {
		s.Reach = ReachAway
	}
	if s.Devices == nil {
		s.Devices = map[string]*Grant{}
	}
	if s.Hide == nil {
		s.Hide = append([]string(nil), DefaultHide...)
	}
	if s.IdleStop == "" {
		s.IdleStop = "15m"
	}
}

func (s *Settings) IdleStopDuration() time.Duration {
	d, err := time.ParseDuration(s.IdleStop)
	if err != nil || d <= 0 {
		return 15 * time.Minute
	}
	return d
}

func dir(agentDir string) string { return filepath.Join(agentDir, settingsDir) }

func settingsPath(agentDir string) string { return filepath.Join(dir(agentDir), "settings.json") }
func ticketsPath(agentDir string) string  { return filepath.Join(dir(agentDir), "tickets.json") }
func hostPath(agentDir string) string     { return filepath.Join(dir(agentDir), "host.json") }
func logPath(agentDir string) string      { return filepath.Join(dir(agentDir), "sessions.log") }
func lockPath(agentDir string) string     { return filepath.Join(dir(agentDir), ".lock") }

var mu sync.Mutex

func withLock(agentDir string, fn func() error) error {
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(dir(agentDir), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(lockPath(agentDir), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f)
	return fn()
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(agentDir string) (*Settings, error) {
	var s Settings
	err := withLock(agentDir, func() error { return readJSON(settingsPath(agentDir), &s) })
	s.normalize()
	return &s, err
}

func Update(agentDir string, fn func(*Settings) error) (*Settings, error) {
	var s Settings
	err := withLock(agentDir, func() error {
		if err := readJSON(settingsPath(agentDir), &s); err != nil {
			return err
		}
		s.normalize()
		if err := fn(&s); err != nil {
			return err
		}
		s.normalize()
		return writeJSON(settingsPath(agentDir), &s)
	})
	return &s, err
}

func ParseRole(r string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "control", "full", "on":
		return RoleControl, nil
	case "watch", "view", "watch-only", "readonly":
		return RoleWatch, nil
	case "off", "deny", "none", "no":
		return RoleOff, nil
	}
	return "", fmt.Errorf("role must be control, watch or off, not %q", r)
}

func (s *Settings) RoleFor(device string) string {
	if !s.Enabled {
		return RoleOff
	}
	g, ok := s.Devices[device]
	if !ok || g.Role == "" {
		return RoleAsk
	}
	if s.AskOnMac && g.Role != RoleOff {
		return RoleAsk
	}
	return g.Role
}

type Host struct {
	Port        int       `json:"port"`
	FastPort    int       `json:"fastPort"`
	Fingerprint string    `json:"fingerprint"`
	Name        string    `json:"name,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	At          time.Time `json:"at"`
}

func (h Host) Valid() error {
	if h.Port <= 0 || h.Port > 65535 {
		return errors.New("port out of range")
	}
	if h.FastPort < 0 || h.FastPort > 65535 {
		return errors.New("fast port out of range")
	}
	fp, err := hex.DecodeString(h.Fingerprint)
	if err != nil || len(fp) != sha256.Size {
		return errors.New("fingerprint must be 64 hex characters")
	}
	return nil
}

func RegisterHost(agentDir string, h Host, now time.Time) error {
	if err := h.Valid(); err != nil {
		return err
	}
	h.At = now
	return withLock(agentDir, func() error { return writeJSON(hostPath(agentDir), h) })
}

func ClearHost(agentDir string) error {
	return withLock(agentDir, func() error {
		err := os.Remove(hostPath(agentDir))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

func CurrentHost(agentDir string, now time.Time) (*Host, error) {
	var h Host
	if err := withLock(agentDir, func() error { return readJSON(hostPath(agentDir), &h) }); err != nil {
		return nil, err
	}
	if h.Port == 0 || now.Sub(h.At) > HostStale {
		return nil, nil
	}
	return &h, nil
}

type ticket struct {
	Hash    string    `json:"hash"`
	Device  string    `json:"device"`
	Name    string    `json:"name"`
	Expires time.Time `json:"expires"`
}

func NewTicket() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashTicket(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func MintTicket(agentDir, device, name string, now time.Time) (string, error) {
	t, err := NewTicket()
	if err != nil {
		return "", err
	}
	err = withLock(agentDir, func() error {
		var list []ticket
		if err := readJSON(ticketsPath(agentDir), &list); err != nil {
			list = nil
		}
		kept := list[:0]
		for _, k := range list {
			if now.Before(k.Expires) {
				kept = append(kept, k)
			}
		}
		if len(kept) >= maxTickets {
			kept = kept[len(kept)-maxTickets+1:]
		}
		kept = append(kept, ticket{Hash: hashTicket(t), Device: device, Name: name, Expires: now.Add(TicketTTL)})
		return writeJSON(ticketsPath(agentDir), kept)
	})
	return t, err
}

var ErrTicket = errors.New("this screen ticket is unknown, used or expired")

func ConsumeTicket(agentDir, t string, now time.Time) (device, name string, err error) {
	want := hashTicket(strings.TrimSpace(t))
	err = withLock(agentDir, func() error {
		var list []ticket
		if err := readJSON(ticketsPath(agentDir), &list); err != nil {
			return err
		}
		found := -1
		for i, k := range list {
			if subtle.ConstantTimeCompare([]byte(k.Hash), []byte(want)) == 1 {
				found = i
			}
		}
		if found < 0 {
			return ErrTicket
		}
		k := list[found]
		list = append(list[:found], list[found+1:]...)
		if werr := writeJSON(ticketsPath(agentDir), list); werr != nil {
			return werr
		}
		if !now.Before(k.Expires) {
			return ErrTicket
		}
		device, name = k.Device, k.Name
		return nil
	})
	return device, name, err
}

func SignedMessage(device string, ts int64) []byte {
	return []byte(signPrefix + "|" + device + "|" + strconv.FormatInt(ts, 10))
}

var seenSigs = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

var ErrSignature = errors.New("Face ID proof did not check out: unlock the app and try again")

func VerifySignature(keyB64, device string, ts int64, sigB64 string, now time.Time) error {
	when := time.Unix(ts, 0)
	if when.Before(now.Add(-SignWindow)) || when.After(now.Add(SignWindow)) {
		return fmt.Errorf("the phone's clock is off by more than %s", SignWindow)
	}
	der, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return ErrSignature
	}
	pub, err := parseKey(der)
	if err != nil {
		return ErrSignature
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return ErrSignature
	}
	digest := sha256.Sum256(SignedMessage(device, ts))
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return ErrSignature
	}
	seenSigs.Lock()
	defer seenSigs.Unlock()
	for k, at := range seenSigs.m {
		if now.Sub(at) > 2*SignWindow {
			delete(seenSigs.m, k)
		}
	}
	if _, dup := seenSigs.m[sigB64]; dup {
		return errors.New("this Face ID proof was already used")
	}
	seenSigs.m[sigB64] = now
	return nil
}

func parseKey(der []byte) (*ecdsa.PublicKey, error) {
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if pub, ok := k.(*ecdsa.PublicKey); ok {
			return pub, nil
		}
		return nil, errors.New("not an EC key")
	}
	if len(der) == 65 && der[0] == 4 {
		return ecdhToECDSA(der)
	}
	return nil, errors.New("unreadable key")
}

func ValidKey(keyB64 string) bool {
	der, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return false
	}
	_, err = parseKey(der)
	return err == nil
}

func IsNearbyAddr(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

type Candidate struct {
	Addr string `json:"addr"`
	Kind string `json:"kind"`
}

var tailnet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

func classify(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return ""
	case tailnet.Contains(ip):
		return "tailnet"
	case ip.To4() != nil && ip.IsPrivate():
		return "lan"
	case ip.To4() == nil && ip.IsPrivate():
		return "lan6"
	case ip.IsLinkLocalUnicast():
		return ""
	case ip.IsGlobalUnicast():
		if ip.To4() == nil {
			return "ipv6"
		}
		return "public"
	}
	return ""
}

func localIPs() []net.IP {
	ifaces, _ := net.Interfaces()
	var out []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				out = append(out, n.IP)
			}
		}
	}
	return out
}

func Candidates(port int, reach string) []Candidate {
	return candidatesFrom(localIPs(), port, reach)
}

func candidatesFrom(ips []net.IP, port int, reach string) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	for _, ip := range ips {
		kind := classify(ip)
		nearbyOnly := reach == ReachNearby && kind != "lan" && kind != "lan6"
		addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
		if kind == "" || nearbyOnly || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, Candidate{Addr: addr, Kind: kind})
	}
	rank := map[string]int{"lan": 0, "lan6": 1, "tailnet": 2, "ipv6": 3, "public": 4}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Kind] < rank[out[j].Kind] })
	return out
}

type Session struct {
	Device string    `json:"device"`
	Name   string    `json:"name"`
	Role   string    `json:"role"`
	Addr   string    `json:"addr,omitempty"`
	Event  string    `json:"event"`
	At     time.Time `json:"at"`
}

func Log(agentDir string, s Session) error {
	if err := os.MkdirAll(dir(agentDir), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath(agentDir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func Recent(agentDir string, n int) ([]Session, error) {
	data, err := os.ReadFile(logPath(agentDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []Session
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		var s Session
		if json.Unmarshal([]byte(lines[i]), &s) == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func ecdhToECDSA(raw []byte) (*ecdsa.PublicKey, error) {
	if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(raw[1:33]), Y: new(big.Int).SetBytes(raw[33:65])}, nil
}
