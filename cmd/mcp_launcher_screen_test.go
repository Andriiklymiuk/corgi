package cmd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/screen"
)

type screenRig struct {
	dir    string
	tokens map[string]string
	hashes map[string]string
}

func newScreenRig(t *testing.T) *screenRig {
	t.Helper()
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir := filepath.Join(data, "agent")
	rig := &screenRig{dir: dir, tokens: map[string]string{}, hashes: map[string]string{}}
	store := &pairing.Store{Version: 1}
	add := func(label, name, role, family string) {
		tok, err := pairing.NewDeviceToken()
		if err != nil {
			t.Fatal(err)
		}
		rig.tokens[label] = tok
		rig.hashes[label] = pairing.HashToken(tok)
		store.Devices = append(store.Devices, pairing.Device{Name: name, TokenHash: pairing.HashToken(tok), CreatedAt: time.Now(), Role: role, Family: family})
	}
	add("ipad", "iPad mini", "", "")
	add("viewer", "Shared iPad", pairing.RoleViewer, "")
	add("peer", "Office Mac", pairing.RolePeer, "")
	add("connector", "claude.ai", "", "fam-1")
	add("bar", "Corgi Agents for Mac (Studio)", "", "")
	if err := pairing.Save(pairing.StorePath(dir), store); err != nil {
		t.Fatal(err)
	}
	return rig
}

func (rig *screenRig) call(t *testing.T, h http.HandlerFunc, who, method, path, body, remote string, headers ...string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rig.tokens[who])
	if remote != "" {
		req.RemoteAddr = remote
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const local = "127.0.0.1:50000"

func (rig *screenRig) registerHost(t *testing.T) {
	t.Helper()
	body := `{"do":"register","port":4100,"fastPort":4101,"fingerprint":"` + strings.Repeat("cd", 32) + `"}`
	if code, out := rig.call(t, launchScreenHostHandler, "bar", "POST", "/launch/screen/host", body, local); code != 200 || out["enabled"] != true {
		t.Fatalf("register: %d %v", code, out)
	}
}

type faceKey struct {
	k   *ecdsa.PrivateKey
	pub string
}

func newFaceKey(t *testing.T) faceKey {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return faceKey{k: k, pub: base64.StdEncoding.EncodeToString(der)}
}

func (f faceKey) start(t *testing.T, device string, withKey bool) string {
	ts := time.Now().Unix()
	d := sha256.Sum256(screen.SignedMessage(device, ts))
	sig, _ := ecdsa.SignASN1(rand.Reader, f.k, d[:])
	m := map[string]any{"do": "start", "width": 1133, "height": 744, "scale": 200, "fps": 120, "ts": ts, "sig": base64.StdEncoding.EncodeToString(sig)}
	if withKey {
		m["key"] = f.pub
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestScreenOffUntilTurnedOn(t *testing.T) {
	rig := newScreenRig(t)
	fk := newFaceKey(t)
	code, out := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], true), "")
	if code != 403 || !strings.Contains(out["error"].(string), "off") {
		t.Fatalf("off by default: %d %v", code, out)
	}
	code, out = rig.call(t, launchScreenHostHandler, "bar", "POST", "/launch/screen/host", `{"do":"register","port":4100,"fingerprint":"`+strings.Repeat("cd", 32)+`"}`, local)
	if code != 200 || out["enabled"] != false {
		t.Fatalf("host registers nothing while off: %d %v", code, out)
	}
}

func TestScreenFullFlow(t *testing.T) {
	rig := newScreenRig(t)
	if err := runAgentScreen(rig.dir, "on", nil); err != nil {
		t.Fatal(err)
	}
	rig.registerHost(t)
	fk := newFaceKey(t)

	code, out := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], false), "")
	if code != 400 {
		t.Fatalf("first start without a key: %d %v", code, out)
	}
	code, out = rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], true), "")
	if code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	if out["role"] != screen.RoleAsk || out["fingerprint"] != strings.Repeat("cd", 32) || out["port"].(float64) != 4100 || out["fastPort"].(float64) != 4101 {
		t.Fatalf("ticket answer: %v", out)
	}
	ticket := out["ticket"].(string)

	code, v := rig.call(t, launchScreenVerifyHandler, "bar", "POST", "/launch/screen/verify", `{"ticket":"`+ticket+`","addr":"192.168.1.9:6000"}`, local)
	if code != 200 || v["role"] != screen.RoleAsk || v["name"] != "iPad mini" {
		t.Fatalf("verify: %d %v", code, v)
	}
	_, again := rig.call(t, launchScreenVerifyHandler, "bar", "POST", "/launch/screen/verify", `{"ticket":"`+ticket+`","addr":"192.168.1.9:6000"}`, local)
	if again["role"] != screen.RoleOff {
		t.Fatalf("a ticket works once: %v", again)
	}

	grant := `{"do":"grant","device":"` + rig.hashes["ipad"] + `","role":"control"}`
	if code, _ := rig.call(t, launchScreenSettingsHandler, "bar", "POST", "/launch/screen/settings", grant, local); code != 200 {
		t.Fatalf("grant: %d", code)
	}
	code, out = rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], false), "")
	if code != 200 || out["role"] != screen.RoleControl {
		t.Fatalf("second start uses the saved key: %d %v", code, out)
	}

	other := newFaceKey(t)
	code, out = rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", other.start(t, rig.hashes["ipad"], true), "")
	if code != 403 || !strings.Contains(out["error"].(string), "key changed") {
		t.Fatalf("a different key is refused: %d %v", code, out)
	}
	code, _ = rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", other.start(t, rig.hashes["ipad"], false), "")
	if code != 403 {
		t.Fatalf("a proof from another key is refused: %d", code)
	}

	_, st := rig.call(t, launchScreenHandler, "ipad", "GET", "/launch/screen", "", "")
	if st["enabled"] != true || st["role"] != screen.RoleControl || st["host"] != true || st["hasKey"] != true {
		t.Fatalf("status: %v", st)
	}

	if err := runAgentScreen(rig.dir, "off", nil); err != nil {
		t.Fatal(err)
	}
	if h, _ := screen.CurrentHost(rig.dir, time.Now()); h != nil {
		t.Fatal("off clears the host")
	}
}

func TestScreenRefusesTheWrongDevices(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	rig.registerHost(t)
	fk := newFaceKey(t)
	for _, who := range []string{"viewer", "connector", "bar"} {
		code, _ := rig.call(t, launchScreenHandler, who, "POST", "/launch/screen", fk.start(t, rig.hashes[who], true), "")
		if code != 403 {
			t.Errorf("%s got %d", who, code)
		}
	}
	code, _ := rig.call(t, launchScreenHandler, "nobody", "GET", "/launch/screen", "", "")
	if code != 401 {
		t.Errorf("no token got %d", code)
	}
}

func TestScreenMacRoutesOnlyFromTheBarOnThisMac(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	body := `{"do":"register","port":4100,"fingerprint":"` + strings.Repeat("cd", 32) + `"}`
	for name, c := range map[string]struct {
		who, remote string
		headers     []string
	}{
		"phone token":           {"ipad", local, nil},
		"bar from the network":  {"bar", "192.168.1.5:7000", nil},
		"bar through a tunnel":  {"bar", local, []string{"Cf-Connecting-Ip", "1.2.3.4"}},
		"bar behind a proxy":    {"bar", local, []string{"X-Forwarded-For", "1.2.3.4"}},
		"viewer on this Mac":    {"viewer", local, nil},
		"connector on this Mac": {"connector", local, nil},
	} {
		for _, h := range []http.HandlerFunc{launchScreenHostHandler, launchScreenVerifyHandler, launchScreenSettingsHandler} {
			code, _ := rig.call(t, h, c.who, "POST", "/launch/screen/x", body, c.remote, c.headers...)
			if code != 403 {
				t.Errorf("%s: %d", name, code)
			}
		}
	}
}

func TestScreenNearbyOnly(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	_ = runAgentScreen(rig.dir, "reach", []string{screen.ReachNearby})
	rig.registerHost(t)
	fk := newFaceKey(t)
	_, out := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", fk.start(t, rig.hashes["ipad"], true), "")
	for _, c := range out["candidates"].([]any) {
		kind := c.(map[string]any)["kind"]
		if kind != "lan" && kind != "lan6" {
			t.Errorf("nearby offered %v", c)
		}
	}
	_, v := rig.call(t, launchScreenVerifyHandler, "bar", "POST", "/launch/screen/verify", `{"ticket":"`+out["ticket"].(string)+`","addr":"100.90.1.2:5000"}`, local)
	if v["role"] != screen.RoleOff {
		t.Fatalf("away address passed nearby-only: %v", v)
	}
}

func TestScreenPowerNeedsControl(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	code, _ := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", `{"do":"sleep"}`, "")
	if code != 403 {
		t.Fatalf("an ungranted device slept the Mac: %d", code)
	}
}

func TestScreenCLIGrantByName(t *testing.T) {
	rig := newScreenRig(t)
	if err := runAgentScreen(rig.dir, "grant", []string{"ipad", "watch"}); err != nil {
		t.Fatal(err)
	}
	s, _ := screen.Load(rig.dir)
	if s.Devices[rig.hashes["ipad"]].Role != screen.RoleWatch {
		t.Fatal("grant by a piece of the name")
	}
	if err := runAgentScreen(rig.dir, "grant", []string{"Office Mac", "control"}); err != nil {
		t.Fatalf("a peer Mac can be granted the screen: %v", err)
	}
	if err := runAgentScreen(rig.dir, "grant", []string{"Shared iPad", "control"}); err == nil {
		t.Fatal("a viewer-only device cannot be granted the screen")
	}
	if err := runAgentScreen(rig.dir, "forget", []string{"iPad mini"}); err != nil {
		t.Fatal(err)
	}
	s, _ = screen.Load(rig.dir)
	if _, ok := s.Devices[rig.hashes["ipad"]]; ok {
		t.Fatal("forget drops the grant")
	}
}

func TestScreenPowerWithControl(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	var ran []string
	prev := screenPowerRun
	screenPowerRun = func(do string) error { ran = append(ran, do); return nil }
	t.Cleanup(func() { screenPowerRun = prev })
	_ = applyScreenChange(rig.dir, screenSettingsIn{Do: "grant", Device: rig.hashes["ipad"], Role: "control"}, time.Now())
	for _, do := range []string{"sleep", "wake"} {
		if code, out := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", `{"do":"`+do+`"}`, ""); code != 200 || out["do"] != do {
			t.Fatalf("%s: %d %v", do, code, out)
		}
	}
	if len(ran) != 2 || ran[0] != "sleep" {
		t.Fatalf("ran %v", ran)
	}
	if code, _ := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", `{"do":"dance"}`, ""); code != 400 {
		t.Fatalf("unknown do: %d", code)
	}
	if code, _ := rig.call(t, launchScreenHandler, "ipad", "POST", "/launch/screen", `not json`, ""); code != 400 {
		t.Fatalf("bad body: %d", code)
	}
	if code, _ := rig.call(t, launchScreenHandler, "ipad", "DELETE", "/launch/screen", "", ""); code != 405 {
		t.Fatalf("delete: %d", code)
	}
}

func TestScreenMacRoutesShapes(t *testing.T) {
	rig := newScreenRig(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	if code, _ := rig.call(t, launchScreenHostHandler, "bar", "GET", "/launch/screen/host", "", local); code != 405 {
		t.Fatalf("host GET: %d", code)
	}
	if code, _ := rig.call(t, launchScreenHostHandler, "bar", "POST", "/launch/screen/host", "{", local); code != 400 {
		t.Fatalf("host bad json: %d", code)
	}
	if code, _ := rig.call(t, launchScreenHostHandler, "bar", "POST", "/launch/screen/host", `{"do":"register","port":70000,"fingerprint":"x"}`, local); code != 400 {
		t.Fatalf("host bad port: %d", code)
	}
	rig.registerHost(t)
	if code, out := rig.call(t, launchScreenHostHandler, "bar", "POST", "/launch/screen/host", `{"do":"clear"}`, local); code != 200 || out["ok"] != true {
		t.Fatalf("clear: %d %v", code, out)
	}
	if code, _ := rig.call(t, launchScreenVerifyHandler, "bar", "GET", "/launch/screen/verify", "", local); code != 405 {
		t.Fatalf("verify GET: %d", code)
	}
	if code, _ := rig.call(t, launchScreenVerifyHandler, "bar", "POST", "/launch/screen/verify", "{", local); code != 400 {
		t.Fatalf("verify bad json: %d", code)
	}
	code, out := rig.call(t, launchScreenSettingsHandler, "bar", "GET", "/launch/screen/settings", "", local)
	if code != 200 || out["settings"] == nil || out["devices"] == nil {
		t.Fatalf("settings GET: %d %v", code, out)
	}
	if code, _ := rig.call(t, launchScreenSettingsHandler, "bar", "POST", "/launch/screen/settings", "{", local); code != 400 {
		t.Fatalf("settings bad json: %d", code)
	}
	if code, _ := rig.call(t, launchScreenSettingsHandler, "bar", "POST", "/launch/screen/settings", `{"do":"explode"}`, local); code != 400 {
		t.Fatalf("settings unknown do: %d", code)
	}
	set := `{"do":"set","settings":{"enabled":true,"reach":"nearby","clipboard":true,"idleStop":"30m"}}`
	code, out = rig.call(t, launchScreenSettingsHandler, "bar", "POST", "/launch/screen/settings", set, local)
	if code != 200 {
		t.Fatalf("set: %d %v", code, out)
	}
	s, _ := screen.Load(rig.dir)
	if s.Reach != screen.ReachNearby || !s.Clipboard || s.IdleStopDuration() != 30*time.Minute {
		t.Fatalf("set did not stick: %+v", s)
	}
	if err := applyScreenChange(rig.dir, screenSettingsIn{Do: "log", Device: "d", Name: "iPad", Event: "end"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := applyScreenChange(rig.dir, screenSettingsIn{Do: "grant", Device: "d", Role: "root"}, time.Now()); err == nil {
		t.Fatal("bad role granted")
	}
	if code, out := rig.call(t, launchScreenSettingsHandler, "bar", "POST", "/launch/screen/settings", `{"do":"set","settings":{"enabled":false}}`, local); code != 200 || out["host"] != nil {
		t.Fatalf("off clears the host: %d %v", code, out)
	}
	if deviceNameFor(rig.dir, rig.hashes["ipad"]) != "iPad mini" || deviceNameFor(rig.dir, "nope") != "" {
		t.Fatal("device names")
	}
}

func TestScreenCLIWords(t *testing.T) {
	rig := newScreenRig(t)
	for _, words := range [][]string{{"status"}, {"devices"}, {"on"}, {"status"}, {"log"}, {"off"}} {
		if err := runAgentScreen(rig.dir, words[0], words[1:]); err != nil {
			t.Fatalf("%v: %v", words, err)
		}
	}
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })
	for _, w := range []string{"status", "devices", "log", "on", "off"} {
		if err := runAgentScreen(rig.dir, w, nil); err != nil {
			t.Fatalf("%s --json: %v", w, err)
		}
	}
	for _, bad := range [][]string{{"grant"}, {"grant", "iPad", "root"}, {"forget"}, {"reach", "mars"}, {"explode"}, {"grant", "nobody", "watch"}} {
		if err := runAgentScreen(rig.dir, bad[0], bad[1:]); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
	screenLogEvent, screenLogDevice = "end", "dev"
	t.Cleanup(func() { screenLogEvent, screenLogDevice = "", "" })
	if err := runAgentScreen(rig.dir, "log", nil); err != nil {
		t.Fatal(err)
	}
	if recent, _ := screen.Recent(rig.dir, 5); len(recent) != 1 {
		t.Fatalf("logged: %v", recent)
	}
}
