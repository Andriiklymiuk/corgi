package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/push"
)

// The hotspot hop: hotel Wi-Fi drops, the laptop in the bag goes quiet, and
// the phone in the same bag has data. After the internet has been gone for a
// while the daemon joins the hotspot named in the config, tries again on a
// backoff, and says so when the gap closes. A budget keeps a dependency
// download from eating the phone's plan: past it, no new run starts until
// Wi-Fi is back.
const DefaultHotspotBudget = 2 << 30

const (
	networkTick      = 30 * time.Second
	offlineBeforeHop = 90 * time.Second
	captiveProbe     = "http://captive.apple.com/hotspot-detect.html"
)

var hopBackoff = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}

type hopStep struct {
	Join bool
	Say  string
	Hold bool
	Back bool
}

type hopper struct {
	ssid         string
	budget       uint64
	offlineSince time.Time
	attempts     int
	nextTry      time.Time
	onHotspot    bool
	hotspotSince time.Time
	bytesAtHop   uint64
	used         uint64
	overBudget   bool
	lastSSID     string
}

// A phone's hotspot carries the phone's name; the first time this Mac is on
// one, that is the hotspot to hop to from then on.
func looksLikePhoneHotspot(ssid string) bool {
	l := strings.ToLower(ssid)
	for _, word := range []string{"iphone", "ipad", "pixel", "galaxy", "android", "hotspot"} {
		if strings.Contains(l, word) {
			return true
		}
	}
	return false
}

// A blip of a few minutes is the Wi-Fi changing rooms, not news.
const offlineWorthSaying = 10 * time.Minute

func onHotspotName(current, hotspot string) bool {
	c, h := strings.ToLower(strings.TrimSpace(current)), strings.ToLower(strings.TrimSpace(hotspot))
	return c != "" && h != "" && (c == h || strings.Contains(c, h))
}

// tick is one look at the network: online or not, what Wi-Fi this is, and
// the interface's byte counter so far.
func (h *hopper) tick(now time.Time, online bool, current string, bytes uint64) hopStep {
	var step hopStep
	wasOnHotspot := h.onHotspot
	h.onHotspot = onHotspotName(current, h.ssid)
	if h.onHotspot && !wasOnHotspot {
		h.hotspotSince, h.bytesAtHop, h.used = now, bytes, 0
	}
	if h.onHotspot && bytes >= h.bytesAtHop {
		h.used = bytes - h.bytesAtHop
	}
	if online {
		if !h.offlineSince.IsZero() {
			gap := now.Sub(h.offlineSince)
			h.offlineSince, h.attempts, h.nextTry = time.Time{}, 0, time.Time{}
			step.Back = true
			switch {
			case h.onHotspot:
				step.Say = "offline for " + roughSpan(gap) + " - hopped onto the hotspot " + strings.TrimSpace(current)
			case gap >= offlineWorthSaying:
				step.Say = "back online after " + roughSpan(gap)
			}
		} else if wasOnHotspot && !h.onHotspot && current != "" {
			step.Say = "back on " + strings.TrimSpace(current) + " after " + roughSpan(now.Sub(h.hotspotSince)) + " on the hotspot (" + sizeOf(h.used) + " used)"
			h.overBudget = false
		}
		if h.onHotspot && h.budget > 0 && h.used >= h.budget && !h.overBudget {
			h.overBudget = true
			step.Say = "the hotspot has used " + sizeOf(h.used) + " of its " + sizeOf(h.budget) + " budget - no new fix starts until Wi-Fi is back"
		}
		step.Hold = h.onHotspot && h.budget > 0 && h.used >= h.budget
		h.lastSSID = current
		return step
	}
	if h.offlineSince.IsZero() {
		h.offlineSince = now
		h.nextTry = now.Add(offlineBeforeHop)
		return step
	}
	if h.ssid == "" || h.onHotspot || now.Before(h.nextTry) {
		return step
	}
	wait := hopBackoff[min(h.attempts, len(hopBackoff)-1)]
	h.attempts++
	h.nextTry = now.Add(wait)
	step.Join = true
	return step
}

func SizeWords(b uint64) string { return sizeOf(b) }

func sizeOf(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%d MB", b/(1<<20))
	}
	return fmt.Sprintf("%d KB", b/(1<<10))
}

// ParseSize reads "2GB", "500MB", "0" or "off"; 0 means no budget.
func ParseSize(s string) (uint64, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if s == "" || s == "0" || s == "OFF" || s == "NONE" {
		return 0, nil
	}
	mult := uint64(1)
	for _, u := range []struct {
		suffix string
		mult   uint64
	}{{"GB", 1 << 30}, {"G", 1 << 30}, {"MB", 1 << 20}, {"M", 1 << 20}, {"KB", 1 << 10}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			s, mult = strings.TrimSuffix(s, u.suffix), u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("a size looks like 2GB or 500MB, not %q", s)
	}
	return uint64(n * float64(mult)), nil
}

// What the rest of the daemon reads about the link: offline holds new runs,
// over budget holds them too, on the hotspot the trackers are polled slower.
var link struct {
	offline    atomic.Bool
	onHotspot  atomic.Bool
	overBudget atomic.Bool
}

func setHotspotHold(hold bool) { link.overBudget.Store(hold) }

func hotspotOverBudget(time.Time) bool { return link.overBudget.Load() }

func offline(time.Time) bool { return link.offline.Load() }

func onMeteredLink() bool { return link.onHotspot.Load() }

var readOnline = func() bool {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(captiveProbe)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "Success")
}

func networkCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, name, args...).Output()
	return string(out)
}

func wifiInterfaceFrom(ports string) string {
	lines := strings.Split(ports, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Hardware Port:") && strings.Contains(strings.ToLower(line), "wi-fi") && i+1 < len(lines) {
			if dev, ok := strings.CutPrefix(strings.TrimSpace(lines[i+1]), "Device:"); ok {
				return strings.TrimSpace(dev)
			}
		}
	}
	return ""
}

var readWifiInterface = func() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	return wifiInterfaceFrom(networkCommand("networksetup", "-listallhardwareports"))
}

func ssidFrom(out string) string {
	if _, after, ok := strings.Cut(out, "Current Wi-Fi Network:"); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

var readSSID = func(iface string) string {
	if iface == "" {
		return ""
	}
	return ssidFrom(networkCommand("networksetup", "-getairportnetwork", iface))
}

func bytesFrom(netstat string) uint64 {
	for _, line := range strings.Split(netstat, "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 10 || !strings.HasPrefix(f[2], "<Link") {
			continue
		}
		in, _ := strconv.ParseUint(f[6], 10, 64)
		out, _ := strconv.ParseUint(f[9], 10, 64)
		return in + out
	}
	return 0
}

var readBytes = func(iface string) uint64 {
	if iface == "" {
		return 0
	}
	return bytesFrom(networkCommand("netstat", "-ib", "-I", iface))
}

// The password lives in the login keychain, never in a file of corgi's.
var hotspotPassword = func(ssid string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "corgi-hotspot", "-a", ssid, "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var joinNetwork = func(iface, ssid, password string) error {
	args := []string{"-setairportnetwork", iface, ssid}
	if password != "" {
		args = append(args, password)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "networksetup", args...).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("%v: %s", err, msg)
	}
	for _, bad := range []string{"could not find", "failed", "error"} {
		if strings.Contains(strings.ToLower(msg), bad) {
			return fmt.Errorf("%s", msg)
		}
	}
	return nil
}

func (d *Daemon) watchNetwork(ctx context.Context) {
	if runtime.GOOS != "darwin" {
		if d.Hotspot != "" {
			utils.Info("agent: hotspot: the hop is macOS only for now")
		}
		return
	}
	h := &hopper{ssid: strings.TrimSpace(d.Hotspot), budget: d.HotspotBudget}
	if h.ssid != "" {
		utils.Infof("agent: hotspot: %s joins when the internet is gone for %s\n", h.ssid, roughSpan(offlineBeforeHop))
	}
	iface := readWifiInterface()
	t := time.NewTicker(networkTick)
	defer t.Stop()
	for {
		now := time.Now()
		online := readOnline()
		ssid := readSSID(iface)
		if h.ssid == "" && online && looksLikePhoneHotspot(ssid) {
			h.ssid = strings.TrimSpace(ssid)
			utils.Infof("agent: hotspot: this is a phone's hotspot - %s is the one to hop to from now on\n", h.ssid)
			if d.LearnHotspot != nil {
				d.LearnHotspot(h.ssid)
			}
		}
		step := h.tick(now, online, ssid, readBytes(iface))
		setHotspotHold(step.Hold)
		link.offline.Store(!online)
		link.onHotspot.Store(h.onHotspot)
		d.setNetwork(online, ssid, h.onHotspot)
		if step.Say != "" {
			d.ringMachine("network", step.Say)
		}
		if step.Back {
			d.flushHeldPushes()
		}
		if step.Join {
			utils.Infof("agent: hotspot: offline for %s - joining %s (try %d)\n", roughSpan(now.Sub(h.offlineSince)), h.ssid, h.attempts)
			if err := joinNetwork(iface, h.ssid, hotspotPassword(h.ssid)); err != nil {
				utils.Infof("agent: hotspot: %v\n", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// A push that could not leave while offline waits here; three or fewer go
// out as they were when the link is back, more become one summary.
const heldPushCap = 20

func (d *Daemon) HoldPush(m push.Message) {
	d.heldPushMu.Lock()
	defer d.heldPushMu.Unlock()
	if len(d.heldPushes) >= heldPushCap {
		d.heldPushes = d.heldPushes[1:]
	}
	d.heldPushes = append(d.heldPushes, m)
}

func (d *Daemon) flushHeldPushes() {
	d.heldPushMu.Lock()
	held := d.heldPushes
	d.heldPushes = nil
	d.heldPushMu.Unlock()
	if len(held) == 0 || d.Push == nil {
		return
	}
	if len(held) <= 3 {
		for _, m := range held {
			go d.Push(m)
		}
		return
	}
	go d.Push(summarizeHeld(held))
}

func summarizeHeld(held []push.Message) push.Message {
	body := fmt.Sprintf("%d while offline:", len(held))
	for i, m := range held {
		if i == 6 {
			body += fmt.Sprintf("\n  and %d more", len(held)-i)
			break
		}
		line := strings.TrimSpace(m.Body)
		if nl := strings.IndexByte(line, '\n'); nl > 0 {
			line = line[:nl]
		}
		body += "\n  " + line
	}
	return push.Message{Title: "corgi agent · while offline", Body: body, Category: "inbox", Thread: "offline", Data: map[string]string{"offline": "1"}}
}

func (d *Daemon) setNetwork(online bool, ssid string, onHotspot bool) {
	state := "offline"
	switch {
	case online && onHotspot:
		state = "hotspot"
	case online && ssid != "":
		state = "wifi"
	case online:
		state = "online"
	}
	d.laptopMu.Lock()
	changed := d.laptop.Network != state || d.laptop.Hotspot != ssid
	d.laptop.Network, d.laptop.Hotspot = state, ssid
	d.laptopMu.Unlock()
	if changed {
		d.requestPublish()
	}
}
