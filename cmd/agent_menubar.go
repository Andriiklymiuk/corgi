package cmd

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// The menu bar app is its own program with its own login item; corgi only
// knows whether it is installed and running, and can open it. Opening it once
// is enough for login: corgi-bar turns its login item on the first time it
// runs from /Applications.
const menuBarAppPath = "/Applications/corgi-bar.app"

var lookForMenuBar = func() (installed, running bool) {
	if runtime.GOOS != "darwin" {
		return false, false
	}
	if _, err := os.Stat(menuBarAppPath); err != nil {
		return false, false
	}
	out, err := exec.Command("pgrep", "-x", "corgi-bar").Output()
	return true, err == nil && strings.TrimSpace(string(out)) != ""
}

var openMenuBar = func() error {
	return exec.Command("open", "-g", "-a", menuBarAppPath).Run()
}

// startMenuBarIfInstalled opens corgi-bar when it is installed and not up,
// so `agent up --at-login` leaves the whole setup coming back after a reboot,
// not just the daemon. Says nothing when the app is not installed: the menu
// bar is optional, and setup already offers it.
func startMenuBarIfInstalled(say func(string)) {
	installed, running := lookForMenuBar()
	if !installed || running {
		return
	}
	if err := openMenuBar(); err != nil {
		say("⚠ could not open corgi-bar: " + err.Error())
		return
	}
	say("✓ opened corgi-bar — it starts at login from now on (Settings › App to change)")
}

const checkMenuBar = "menu bar"

func checkMenuBarApp() agentCheck {
	if runtime.GOOS != "darwin" {
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "not applicable"}
	}
	installed, running := lookForMenuBar()
	switch {
	case !installed:
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "corgi-bar not installed (optional)", Fix: "`brew install --cask andriiklymiuk/tools/corgi-bar`"}
	case !running:
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "corgi-bar installed, not running", Fix: "`open -a corgi-bar` — its first run turns login start on"}
	}
	return agentCheck{Name: checkMenuBar, OK: true, Detail: "corgi-bar running"}
}
