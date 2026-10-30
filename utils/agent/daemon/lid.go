package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/supervisor"
)

// The lid guard: a laptop closed and left in a room should say when it is
// opened. macOS freezes the daemon with the lid, so a gap between two ticks
// is the sleep, and the tick after it is the opening. The clamshell state
// catches an opening that did not sleep the machine.

const (
	guardTick       = 5 * time.Second
	guardSleptAfter = 90 * time.Second
	guardRepeatGap  = 10 * time.Minute
)

var readClamshell = func() (closed bool, known bool) {
	if runtime.GOOS != "darwin" {
		return false, false
	}
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "4").Output()
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "AppleClamshellState") {
			return strings.Contains(line, "Yes"), true
		}
	}
	return false, false
}

func (d *Daemon) watchLid(ctx context.Context) {
	if runtime.GOOS != "darwin" {
		return
	}
	g := lidGuard{last: time.Now()}
	if hours := strings.TrimSpace(d.Lid); hours != "" {
		window, err := ParseQuiet(hours)
		if err != nil {
			utils.Infof("agent: lid: %v\n", err)
		} else {
			g.window = &window
			utils.Infof("agent: lid: a wake or an open lid between %s rings the phone\n", hours)
		}
	}
	if closed, known := readClamshell(); known {
		g.closed = closed
		d.lidClosed.Store(closed)
		lidClosedNow.Store(closed)
		d.setLid(closed)
	}
	t := time.NewTicker(guardTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			closed, known := readClamshell()
			was := g.closed
			slept := now.Sub(g.last) >= guardSleptAfter
			if body := g.observe(now, closed, known); body != "" {
				d.ringLid(body)
			}
			if slept {
				d.rearmAfterSleep()
			}
			if known && closed != was {
				d.lidClosed.Store(closed)
				lidClosedNow.Store(closed)
				d.setLid(closed)
				d.lidChanged(closed)
			}
		}
	}
}

func (d *Daemon) lidIsClosed() bool { return d.lidClosed.Load() }

func (d *Daemon) rearmAfterSleep() {
	if !supervisor.PmsetAllowed() {
		return
	}
	if err := supervisor.RearmAfterSleep(d.Dir); err != nil {
		utils.Infof("agent: sleep: %v\n", err)
	}
}

func (d *Daemon) setLid(closed bool) {
	state := "open"
	if closed {
		state = "closed"
	}
	d.laptopMu.Lock()
	changed := d.laptop.Lid != state
	d.laptop.Lid = state
	d.laptopMu.Unlock()
	if changed {
		d.requestPublish()
	}
}

type lidGuard struct {
	window   *QuietHours
	last     time.Time
	closed   bool
	lastRing time.Time
}

// observe is one tick: it says what to tell the phone, or "".
func (g *lidGuard) observe(now time.Time, closed, known bool) string {
	slept := now.Sub(g.last)
	wasClosed := g.closed
	g.last = now
	if known {
		g.closed = closed
	}
	opened := known && wasClosed && !closed
	woke := slept >= guardSleptAfter && (!known || !closed)
	if !opened && !woke {
		return ""
	}
	if g.window == nil || !g.window.Contains(now) || now.Sub(g.lastRing) < guardRepeatGap {
		return ""
	}
	g.lastRing = now
	at := now.Format("15:04")
	switch {
	case opened && slept >= guardSleptAfter:
		return "the laptop's lid was opened at " + at + " after " + roughSpan(slept) + " asleep"
	case opened:
		return "the laptop's lid was opened at " + at
	default:
		return "the laptop woke up at " + at + " after " + roughSpan(slept) + " asleep"
	}
}

func roughSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return plural(h, "hour") + " " + plural(m, "minute")
		}
		return plural(h, "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func (d *Daemon) ringLid(body string) {
	utils.Infof("agent: lid: %s\n", body)
	if d.muted() {
		return
	}
	if d.Push != nil {
		go d.Push(push.Message{Title: "corgi agent · laptop", Body: body, Category: "lid", Data: map[string]string{"needs": "1", "lid": "1", "at": time.Now().Format("15:04")}, Thread: "lid"})
	}
	if d.Notify != nil {
		d.Notify("corgi agent · laptop", body)
	}
}

// What a closed lid changes, Insomnia-style, and an open one undoes: the
// screen is let go (a lit screen in a closed laptop is heat and no lock)
// and the sound is muted. A second display in
// clamshell mode is a screen someone looks at, so nothing changes then.
var hasExternalDisplay = func() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return false
	}
	return externalDisplayIn(string(out))
}

func externalDisplayIn(profile string) bool {
	for _, line := range strings.Split(profile, "\n") {
		if strings.Contains(line, "Connection Type:") && !strings.Contains(line, "Internal") {
			return true
		}
	}
	return false
}

var readMuted = func() bool {
	out, err := exec.Command("osascript", "-e", "output muted of (get volume settings)").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

var setMuted = func(on bool) {
	_ = exec.Command("osascript", "-e", fmt.Sprintf("set volume output muted %t", on)).Run()
}

func (d *Daemon) lidChanged(closed bool) {
	if hasExternalDisplay() {
		utils.Info("agent: lid: a second display is on - the laptop is left as it is")
		return
	}
	if closed {
		if d.Awake != nil && d.Awake.Display() {
			_ = d.Awake.SetDisplay(false)
			utils.Info("agent: lid: closed - the screen may sleep and lock until it opens")
		}
		if !readMuted() {
			setMuted(true)
			d.lidMuted.Store(true)
		}
		return
	}
	if d.lidMuted.Swap(false) {
		setMuted(false)
	}
	if d.Awake != nil && d.AwakePolicy != nil {
		if _, display := d.AwakePolicy(); display {
			_ = d.Awake.SetDisplay(true)
		}
	}
}

// The phone's answer to a lid ring, when the one who opened the laptop was
// not its owner.

var runGuardCommand = func(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// LidAct carries out what the phone chose. Lock starts the screen saver,
// which locks; shutdown asks the system the way the Apple menu does, so
// open apps close properly. Neither needs root, neither is stopped by a
// caffeinate running in a terminal.
func LidAct(dir, action string) (string, error) {
	switch action {
	case "lock":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("locking from the phone is macOS only for now")
		}
		// The screen saver locks at once under the default "require password"
		// setting; the display goes dark with it. Sleep is not asked for:
		// pmset disablesleep, or a caffeinate in a terminal, would refuse it.
		if err := runGuardCommand("open", "-a", "ScreenSaverEngine"); err != nil {
			return "", err
		}
		go func() { _ = runGuardCommand("pmset", "displaysleepnow") }()
		return "the laptop is locked - the password is asked to get back in", nil
	case "sleep":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("sleeping from here is macOS only for now")
		}
		if err := supervisor.SleepNow(dir); err != nil {
			return "", err
		}
		return "the laptop is going to sleep - it stays closable again when it wakes", nil
	case "shutdown":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("shutting down from the phone is macOS only for now")
		}
		go func() { _ = runGuardCommand("osascript", "-e", `tell application "System Events" to shut down`) }()
		return "the laptop is shutting down", nil
	}
	return "", fmt.Errorf("guard action is lock, sleep or shutdown, not %q", action)
}
