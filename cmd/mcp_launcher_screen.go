package cmd

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/screen"
)

const macBarDevicePrefix = "Corgi Agents for Mac"

func screenPhone(d pairing.Device) bool {
	return !d.Viewer() && !d.Peer() && d.Family == "" && !screenMacBar(d)
}

func screenMacBar(d pairing.Device) bool {
	return d.Family == "" && !d.Viewer() && !d.Peer() && strings.HasPrefix(d.Name, macBarDevicePrefix)
}

func fromThisMachine(r *http.Request) bool {
	for _, h := range []string{"Cf-Connecting-Ip", "X-Forwarded-For", "X-Real-Ip", "Forwarded", "Cf-Ray"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func screenDevice(w http.ResponseWriter, r *http.Request) (string, pairing.Device, bool) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return "", pairing.Device{}, false
	}
	d, ok := authorizedDeviceFull(pairing.StorePath(dir), r.Header.Get("Authorization"))
	if !ok {
		writeLaunchError(w, http.StatusUnauthorized, "pair this device first")
		return "", pairing.Device{}, false
	}
	return dir, d, true
}

func screenMac(w http.ResponseWriter, r *http.Request) (string, bool) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return "", false
	}
	if !screenMacBar(d) || !fromThisMachine(r) {
		writeLaunchError(w, http.StatusForbidden, "only Corgi Bar on this Mac may do that")
		return "", false
	}
	return dir, true
}

type screenStart struct {
	Do     string `json:"do"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Scale  int    `json:"scale"`
	FPS    int    `json:"fps"`
	Key    string `json:"key"`
	TS     int64  `json:"ts"`
	Sig    string `json:"sig"`
}

func macName() string {
	if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			return n
		}
	}
	h, _ := os.Hostname()
	return strings.TrimSuffix(h, ".local")
}

func launchScreenHandler(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return
	}
	if !screenPhone(d) {
		writeLaunchError(w, http.StatusForbidden, "this device cannot open the Mac's screen")
		return
	}
	s, err := screen.Load(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	host, _ := screen.CurrentHost(dir, now)
	if r.Method == http.MethodGet {
		g := s.Devices[d.TokenHash]
		writeLaunchJSON(w, map[string]any{
			"enabled": s.Enabled,
			"role":    s.RoleFor(d.TokenHash),
			"reach":   s.Reach,
			"mac":     macName(),
			"host":    host != nil,
			"hasKey":  g != nil && g.Key != "",
		})
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	var in screenStart
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	switch in.Do {
	case "start":
		screenStartSession(w, dir, d, s, host, in, now)
	case "sleep", "wake":
		screenPower(w, s, d, in.Do)
	default:
		writeLaunchError(w, http.StatusBadRequest, "do must be start, sleep or wake")
	}
}

func screenStartSession(w http.ResponseWriter, dir string, d pairing.Device, s *screen.Settings, host *screen.Host, in screenStart, now time.Time) {
	if !s.Enabled {
		writeLaunchError(w, http.StatusForbidden, "Remote screen is off on this Mac. Turn it on in Corgi Bar › Settings › Remote screen.")
		return
	}
	if host == nil {
		writeLaunchError(w, http.StatusServiceUnavailable, "Open Corgi Bar on the Mac: it shares the screen.")
		return
	}
	g := s.Devices[d.TokenHash]
	key := strings.TrimSpace(in.Key)
	switch {
	case g != nil && g.Key != "" && key != "" && key != g.Key:
		writeLaunchError(w, http.StatusForbidden, "This device's Face ID key changed. Remove it in Corgi Bar › Settings › Remote screen, then try again.")
		return
	case g != nil && g.Key != "":
		key = g.Key
	case key == "" || !screen.ValidKey(key):
		writeLaunchError(w, http.StatusBadRequest, "send the device's Face ID key with the first request")
		return
	}
	if err := screen.VerifySignature(key, d.TokenHash, in.TS, in.Sig, now); err != nil {
		writeLaunchError(w, http.StatusForbidden, err.Error())
		return
	}
	if g == nil || g.Key == "" {
		if _, err := screen.Update(dir, func(st *screen.Settings) error {
			cur := st.Devices[d.TokenHash]
			if cur == nil {
				cur = &screen.Grant{Name: d.Name, Since: now}
				st.Devices[d.TokenHash] = cur
			}
			cur.Key = key
			return nil
		}); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	role := s.RoleFor(d.TokenHash)
	if role == screen.RoleOff {
		writeLaunchError(w, http.StatusForbidden, "This Mac turned remote screen off for "+d.Name+".")
		return
	}
	ticket, err := screen.MintTicket(dir, d.TokenHash, d.Name, now)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{
		"ticket":      ticket,
		"fingerprint": host.Fingerprint,
		"port":        host.Port,
		"fastPort":    host.FastPort,
		"candidates":  screen.Candidates(host.Port, s.Reach),
		"role":        role,
		"mac":         macName(),
		"expires":     now.Add(screen.TicketTTL),
	})
}

func screenPower(w http.ResponseWriter, s *screen.Settings, d pairing.Device, do string) {
	if !s.Enabled || s.RoleFor(d.TokenHash) != screen.RoleControl {
		writeLaunchError(w, http.StatusForbidden, "only a device with control may sleep or wake this Mac")
		return
	}
	var cmd *exec.Cmd
	if do == "sleep" {
		cmd = exec.Command("pmset", "displaysleepnow")
	} else {
		cmd = exec.Command("caffeinate", "-u", "-t", "3")
	}
	if err := cmd.Start(); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	go func() { _ = cmd.Wait() }()
	writeLaunchJSON(w, map[string]any{"ok": true, "do": do})
}

type screenHostIn struct {
	Do          string `json:"do"`
	Port        int    `json:"port"`
	FastPort    int    `json:"fastPort"`
	Fingerprint string `json:"fingerprint"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func launchScreenHostHandler(w http.ResponseWriter, r *http.Request) {
	dir, ok := screenMac(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	var in screenHostIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the host")
		return
	}
	if in.Do == "clear" {
		_ = screen.ClearHost(dir)
		writeLaunchJSON(w, map[string]any{"ok": true})
		return
	}
	s, err := screen.Load(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.Enabled {
		_ = screen.ClearHost(dir)
		writeLaunchJSON(w, map[string]any{"enabled": false})
		return
	}
	h := screen.Host{Port: in.Port, FastPort: in.FastPort, Fingerprint: in.Fingerprint, Width: in.Width, Height: in.Height}
	if err := screen.RegisterHost(dir, h, time.Now()); err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"enabled": true, "settings": s})
}

type screenVerifyIn struct {
	Ticket string `json:"ticket"`
	Addr   string `json:"addr"`
}

func launchScreenVerifyHandler(w http.ResponseWriter, r *http.Request) {
	dir, ok := screenMac(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	var in screenVerifyIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the ticket")
		return
	}
	writeLaunchJSON(w, verifyScreenTicket(dir, in.Ticket, in.Addr, time.Now()))
}

func verifyScreenTicket(dir, ticket, addr string, now time.Time) map[string]any {
	device, name, err := screen.ConsumeTicket(dir, ticket, now)
	if err != nil {
		return map[string]any{"role": screen.RoleOff, "reason": err.Error()}
	}
	s, err := screen.Load(dir)
	if err != nil {
		return map[string]any{"role": screen.RoleOff, "reason": err.Error()}
	}
	role := s.RoleFor(device)
	if s.Reach == screen.ReachNearby && !screen.IsNearbyAddr(addr) {
		return map[string]any{"role": screen.RoleOff, "reason": "this Mac only shares its screen on the same network"}
	}
	if role != screen.RoleOff {
		_ = screen.Log(dir, screen.Session{Device: device, Name: name, Role: role, Addr: addr, Event: "start", At: now})
	}
	return map[string]any{"role": role, "device": device, "name": name, "settings": s}
}

type screenSettingsIn struct {
	Do       string          `json:"do"`
	Device   string          `json:"device"`
	Role     string          `json:"role"`
	Settings json.RawMessage `json:"settings"`
	Event    string          `json:"event"`
	Name     string          `json:"name"`
	Addr     string          `json:"addr"`
}

func launchScreenSettingsHandler(w http.ResponseWriter, r *http.Request) {
	dir, ok := screenMac(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		writeLaunchJSON(w, screenOverview(dir))
		return
	}
	var in screenSettingsIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the settings")
		return
	}
	if err := applyScreenChange(dir, in, time.Now()); err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, screenOverview(dir))
}

func applyScreenChange(dir string, in screenSettingsIn, now time.Time) error {
	switch in.Do {
	case "log":
		return screen.Log(dir, screen.Session{Device: in.Device, Name: in.Name, Role: in.Role, Addr: in.Addr, Event: in.Event, At: now})
	case "set":
		_, err := screen.Update(dir, func(s *screen.Settings) error {
			devices := s.Devices
			if err := json.Unmarshal(in.Settings, s); err != nil {
				return err
			}
			s.Devices = devices
			if !s.Enabled {
				_ = screen.ClearHost(dir)
			}
			return nil
		})
		return err
	case "grant":
		role, err := screen.ParseRole(in.Role)
		if err != nil {
			return err
		}
		_, err = screen.Update(dir, func(s *screen.Settings) error {
			g := s.Devices[in.Device]
			if g == nil {
				g = &screen.Grant{Name: deviceNameFor(dir, in.Device), Since: now}
				s.Devices[in.Device] = g
			}
			g.Role = role
			return nil
		})
		return err
	case "forget":
		_, err := screen.Update(dir, func(s *screen.Settings) error {
			delete(s.Devices, in.Device)
			return nil
		})
		return err
	}
	return errUnknownScreenChange
}

var errUnknownScreenChange = &screenError{"do must be set, grant, forget or log"}

type screenError struct{ msg string }

func (e *screenError) Error() string { return e.msg }

func deviceNameFor(dir, hash string) string {
	store, err := pairing.Load(pairing.StorePath(dir))
	if err != nil {
		return ""
	}
	for _, d := range store.Devices {
		if d.TokenHash == hash {
			return d.Name
		}
	}
	return ""
}

type screenDeviceRow struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Role string    `json:"role"`
	Key  bool      `json:"key"`
	Last time.Time `json:"last,omitempty"`
}

func screenOverview(dir string) map[string]any {
	s, _ := screen.Load(dir)
	var rows []screenDeviceRow
	if store, err := pairing.Load(pairing.StorePath(dir)); err == nil {
		for _, d := range store.Devices {
			if !screenPhone(d) {
				continue
			}
			row := screenDeviceRow{ID: d.TokenHash, Name: d.Name, Role: screen.RoleAsk}
			if g := s.Devices[d.TokenHash]; g != nil {
				if g.Role != "" {
					row.Role = g.Role
				}
				row.Key = g.Key != ""
				row.Last = g.Last
			}
			rows = append(rows, row)
		}
	}
	recent, _ := screen.Recent(dir, 30)
	host, _ := screen.CurrentHost(dir, time.Now())
	return map[string]any{"settings": s, "devices": rows, "recent": recent, "host": host}
}
