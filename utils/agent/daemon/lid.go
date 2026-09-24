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
	if strings.TrimSpace(d.Lid) == "" {
		return
	}
	window, err := ParseQuiet(d.Lid)
	if err != nil {
		utils.Infof("agent: lid: %v\n", err)
		return
	}
	utils.Infof("agent: lid: a wake or an open lid between %s rings the phone\n", d.Lid)
	g := lidGuard{window: window, last: time.Now()}
	t := time.NewTicker(guardTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			closed, known := readClamshell()
			if body := g.observe(now, closed, known); body != "" {
				d.ringLid(body)
			}
		}
	}
}

type lidGuard struct {
	window   QuietHours
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
	if !g.window.Contains(now) || now.Sub(g.lastRing) < guardRepeatGap {
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
	if d.Push != nil {
		go d.Push(push.Message{Title: "corgi agent · laptop", Body: body, Category: "lid", Data: map[string]string{"needs": "1", "lid": "1"}, Thread: "lid"})
	}
	if d.Notify != nil {
		d.Notify("corgi agent · laptop", body)
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
func LidAct(action string) (string, error) {
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
	case "shutdown":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("shutting down from the phone is macOS only for now")
		}
		go func() { _ = runGuardCommand("osascript", "-e", `tell application "System Events" to shut down`) }()
		return "the laptop is shutting down", nil
	}
	return "", fmt.Errorf("guard action is lock or shutdown, not %q", action)
}
