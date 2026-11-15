package screen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsAreSafe(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled {
		t.Fatal("remote screen must be off by default")
	}
	if s.Reach != ReachAway || s.IdleStopDuration() != 15*time.Minute {
		t.Fatalf("defaults: %+v", s)
	}
	if len(s.Hide) == 0 {
		t.Fatal("password managers are hidden by default")
	}
	if s.RoleFor("anything") != RoleOff {
		t.Fatal("off means off for every device")
	}
}

func TestRoles(t *testing.T) {
	dir := t.TempDir()
	s, err := Update(dir, func(s *Settings) error {
		s.Enabled = true
		s.Devices["ipad"] = &Grant{Name: "iPad", Role: RoleControl}
		s.Devices["phone"] = &Grant{Name: "iPhone", Role: RoleWatch}
		s.Devices["old"] = &Grant{Name: "Old", Role: RoleOff}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for dev, want := range map[string]string{"ipad": RoleControl, "phone": RoleWatch, "old": RoleOff, "new": RoleAsk} {
		if got := s.RoleFor(dev); got != want {
			t.Errorf("%s: %s, want %s", dev, got, want)
		}
	}
	s.AskOnMac = true
	if s.RoleFor("ipad") != RoleAsk || s.RoleFor("old") != RoleOff {
		t.Error("ask on Mac asks for every granted device but keeps off as off")
	}
	back, _ := Load(dir)
	if back.Devices["ipad"].Role != RoleControl {
		t.Error("grants persist")
	}
	info, _ := os.Stat(filepath.Join(dir, "screen", "settings.json"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("settings mode %v", info.Mode().Perm())
	}
}

func TestParseRole(t *testing.T) {
	for in, want := range map[string]string{"control": RoleControl, "Watch": RoleWatch, "off": RoleOff, "view": RoleWatch} {
		got, err := ParseRole(in)
		if err != nil || got != want {
			t.Errorf("%s → %s %v", in, got, err)
		}
	}
	if _, err := ParseRole("admin"); err == nil {
		t.Error("unknown role accepted")
	}
}

func TestTicketsAreOneTime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tk, err := MintTicket(dir, "dev1", "iPad", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(tk) < 40 {
		t.Fatalf("ticket too short: %q", tk)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "screen", "tickets.json"))
	if strings.Contains(string(raw), tk) {
		t.Fatal("tickets must be stored hashed")
	}
	dev, name, err := ConsumeTicket(dir, tk, now.Add(time.Second))
	if err != nil || dev != "dev1" || name != "iPad" {
		t.Fatalf("consume: %s %s %v", dev, name, err)
	}
	if _, _, err := ConsumeTicket(dir, tk, now.Add(2*time.Second)); err != ErrTicket {
		t.Fatalf("second use: %v", err)
	}
}

func TestTicketsExpire(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tk, _ := MintTicket(dir, "dev", "x", now)
	if _, _, err := ConsumeTicket(dir, tk, now.Add(TicketTTL+time.Second)); err != ErrTicket {
		t.Fatalf("expired ticket: %v", err)
	}
	if _, _, err := ConsumeTicket(dir, "made-up", now); err != ErrTicket {
		t.Fatalf("made-up ticket: %v", err)
	}
}

func TestTicketsAreCapped(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	first, _ := MintTicket(dir, "dev", "x", now)
	for i := 0; i < maxTickets+5; i++ {
		if _, err := MintTicket(dir, "dev", "x", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := ConsumeTicket(dir, first, now); err != ErrTicket {
		t.Fatal("oldest ticket should have been dropped")
	}
}

func TestHostRegistry(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fp := strings.Repeat("ab", 32)
	if err := RegisterHost(dir, Host{Port: 0, Fingerprint: fp}, now); err == nil {
		t.Fatal("port 0 accepted")
	}
	if err := RegisterHost(dir, Host{Port: 5000, Fingerprint: "zz"}, now); err == nil {
		t.Fatal("bad fingerprint accepted")
	}
	if err := RegisterHost(dir, Host{Port: 5000, FastPort: 5001, Fingerprint: fp}, now); err != nil {
		t.Fatal(err)
	}
	h, _ := CurrentHost(dir, now.Add(10*time.Second))
	if h == nil || h.Port != 5000 || h.FastPort != 5001 {
		t.Fatalf("host: %+v", h)
	}
	if h, _ := CurrentHost(dir, now.Add(HostStale+time.Second)); h != nil {
		t.Fatal("a host that stopped checking in is gone")
	}
	_ = ClearHost(dir)
	if h, _ := CurrentHost(dir, now); h != nil {
		t.Fatal("cleared host still there")
	}
}

func sign(t *testing.T, k *ecdsa.PrivateKey, device string, ts int64) string {
	t.Helper()
	d := sha256.Sum256(SignedMessage(device, ts))
	sig, err := ecdsa.SignASN1(rand.Reader, k, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func TestFaceIDSignature(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pkix := base64.StdEncoding.EncodeToString(der)
	raw := base64.StdEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), k.PublicKey.X, k.PublicKey.Y))
	now := time.Now()
	for _, key := range []string{pkix, raw} {
		if !ValidKey(key) {
			t.Fatal("key format not understood")
		}
		ts := now.Unix()
		sig := sign(t, k, "dev", ts)
		if err := VerifySignature(key, "dev", ts, sig, now); err != nil {
			t.Fatalf("good proof refused: %v", err)
		}
		if err := VerifySignature(key, "dev", ts, sig, now); err == nil {
			t.Fatal("replayed proof accepted")
		}
		if err := VerifySignature(key, "other", ts, sign(t, k, "dev", ts+1), now); err == nil {
			t.Fatal("proof for another device accepted")
		}
		old := now.Add(-5 * time.Minute).Unix()
		if err := VerifySignature(key, "dev", old, sign(t, k, "dev", old), now); err == nil {
			t.Fatal("stale proof accepted")
		}
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ts := now.Unix() + 3
	if err := VerifySignature(pkix, "dev", ts, sign(t, other, "dev", ts), now); err == nil {
		t.Fatal("proof from another key accepted")
	}
	if ValidKey("bm90IGEga2V5") {
		t.Fatal("garbage key accepted")
	}
}

func TestNearby(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.20:5000": true, "10.0.0.2": true, "[fe80::1]:9": true, "[fd00::5]:9": true,
		"127.0.0.1:1": true, "[::ffff:192.168.0.3]:5": true,
		"100.101.102.103:5000": false, "8.8.8.8:53": false, "[2001:db8::1]:9": false, "nonsense": false,
	} {
		if got := IsNearbyAddr(addr); got != want {
			t.Errorf("%s: %v, want %v", addr, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	for ip, want := range map[string]string{
		"192.168.0.2": "lan", "100.100.1.1": "tailnet", "fd12::1": "lan6", "2a01::1": "ipv6", "1.2.3.4": "public", "127.0.0.1": "", "fe80::1": "",
	} {
		if got := classify(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %q, want %q", ip, got, want)
		}
	}
}

func TestCandidatesNearbyOnlyLAN(t *testing.T) {
	for _, c := range Candidates(4242, ReachNearby) {
		if c.Kind != "lan" && c.Kind != "lan6" {
			t.Errorf("nearby offered %+v", c)
		}
		if !strings.HasSuffix(c.Addr, ":4242") {
			t.Errorf("port missing: %s", c.Addr)
		}
	}
}

func TestSessionLog(t *testing.T) {
	dir := t.TempDir()
	for _, e := range []string{"start", "end"} {
		if err := Log(dir, Session{Device: "d", Name: "iPad", Role: RoleControl, Event: e, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Recent(dir, 10)
	if err != nil || len(got) != 2 || got[0].Event != "end" {
		t.Fatalf("recent: %+v %v", got, err)
	}
}

func TestCandidatesFrom(t *testing.T) {
	ips := []net.IP{net.ParseIP("8.8.4.4"), net.ParseIP("192.168.1.7"), net.ParseIP("100.70.1.1"), net.ParseIP("192.168.1.7"), net.ParseIP("127.0.0.1"), net.ParseIP("2a01::5")}
	got := candidatesFrom(ips, 9000, ReachAway)
	if len(got) != 4 || got[0].Kind != "lan" || got[1].Kind != "tailnet" || got[2].Kind != "ipv6" || got[3].Kind != "public" {
		t.Fatalf("away: %+v", got)
	}
	near := candidatesFrom(ips, 9000, ReachNearby)
	if len(near) != 1 || near[0].Addr != "192.168.1.7:9000" {
		t.Fatalf("nearby: %+v", near)
	}
}

func TestBrokenFilesSurface(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "screen"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "screen", "settings.json"), []byte("{nope"), 0o600)
	if _, err := Load(dir); err == nil {
		t.Fatal("a broken settings file is an error, not silent defaults")
	}
	if _, err := Update(dir, func(*Settings) error { return nil }); err == nil {
		t.Fatal("update refuses to overwrite a broken file")
	}
	if VerifySignature("!!", "d", time.Now().Unix(), "x", time.Now()) == nil {
		t.Fatal("garbage key verified")
	}
	if (&Settings{IdleStop: "soon"}).IdleStopDuration() != 15*time.Minute {
		t.Fatal("bad idle stop falls back")
	}
}
