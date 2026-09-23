package cmd

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

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

func startMenuBarIfInstalled(say func(string)) {
	installed, running := lookForMenuBar()
	if !installed || running {
		return
	}
	if err := openMenuBar(); err != nil {
		say("⚠ could not open corgi-bar: " + err.Error())
		return
	}
	say("✓ opened corgi-bar - it starts at login from now on (Settings › App to change)")
}

const checkMenuBar = "menu bar"

func checkMenuBarApp() agentCheck {
	if runtime.GOOS != "darwin" {
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "not applicable"}
	}
	installed, running := lookForMenuBar()
	switch {
	case !installed:
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "Corgi Agents for Mac not installed (optional)", Fix: "install it from the Mac App Store (Corgi Agents)"}
	case !running:
		return agentCheck{Name: checkMenuBar, OK: true, Detail: "corgi-bar installed, not running", Fix: "`open -a corgi-bar` - its first run turns login start on"}
	}
	return agentCheck{Name: checkMenuBar, OK: true, Detail: "corgi-bar running"}
}
