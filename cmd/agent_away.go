package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/push"
)

const (
	awayDiskMinGB   = 20
	awayHotLimit    = 60
	captiveProbeURL = "http://captive.apple.com/hotspot-detect.html"
)

func macUpdatesOff(out string) (off, known bool) {
	switch strings.TrimSpace(out) {
	case "0", "false":
		return true, true
	case "1", "true":
		return false, true
	}
	return false, false
}

func lidSleepDisabled(pmsetG string) bool {
	for _, line := range strings.Split(pmsetG, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "SleepDisabled" && f[1] == "1" {
			return true
		}
	}
	return false
}

func onACPower(batt string) bool { return strings.Contains(batt, "'AC Power'") }

func captiveOK(body string) bool { return strings.Contains(body, "Success") }

var speedLimitLine = regexp.MustCompile(`CPU_Speed_Limit\s*=\s*(\d+)`)

func cpuSpeedLimit(therm string) (int, bool) {
	m := speedLimitLine.FindStringSubmatch(therm)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

func fileVaultOn(out string) bool { return strings.Contains(out, "FileVault is On") }

func awayCommand(name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err == nil
}

func couldNotCheck(name string) agentCheck {
	return agentCheck{Name: name, OK: true, Detail: "could not check"}
}

func awayChecks(dir string) []agentCheck {
	var checks []agentCheck
	if runtime.GOOS == "darwin" {
		checks = append(checks, checkMacUpdates(), checkLid(), checkPower(), checkFileVault(), checkHeat())
	}
	checks = append(checks, checkDisk(dir), checkNetwork(), checkPulse(dir), checkTunnel(dir), checkPhone(dir), checkPeers(dir))
	if user, err := config.LoadUser(agentUserConfigPath(dir)); err == nil && user != nil {
		checks = append(checks, digestChecks(user)...)
		checks = append(checks, isolationChecks(user)...)
	}
	return append(checks, checkClaudeUpdater())
}

func checkMacUpdates() agentCheck {
	const name = "macOS updates"
	out, ok := awayCommand("defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", "AutomaticallyInstallMacOSUpdates")
	off, known := macUpdatesOff(out)
	if !ok || !known {
		return couldNotCheck(name)
	}
	if off {
		return agentCheck{Name: name, OK: true, Detail: "not installed on their own"}
	}
	return agentCheck{Name: name, Detail: "macOS installs updates on its own - after the reboot it waits at the login screen until you are back",
		Fix: "sudo softwareupdate --schedule off && sudo defaults write /Library/Preferences/com.apple.SoftwareUpdate AutomaticallyInstallMacOSUpdates -bool false"}
}

func checkLid() agentCheck {
	const name = "lid"
	out, ok := awayCommand("pmset", "-g")
	if !ok {
		return couldNotCheck(name)
	}
	if lidSleepDisabled(out) {
		return agentCheck{Name: name, OK: true, Detail: "closing the lid does not sleep the machine (SleepDisabled 1)"}
	}
	return agentCheck{Name: name, Detail: "closing the lid sleeps the machine, and everything with it", Fix: "sudo pmset -a disablesleep 1"}
}

func checkPower() agentCheck {
	const name = "power"
	out, ok := awayCommand("pmset", "-g", "batt")
	if !ok {
		return couldNotCheck(name)
	}
	if onACPower(out) {
		return agentCheck{Name: name, OK: true, Detail: "on AC power"}
	}
	return agentCheck{Name: name, Detail: "on battery", Fix: "plug it in"}
}

func checkFileVault() agentCheck {
	const name = "FileVault"
	out, ok := awayCommand("fdesetup", "status")
	if !ok {
		return couldNotCheck(name)
	}
	if fileVaultOn(out) {
		return agentCheck{Name: name, OK: true, Detail: "on - a reboot waits at the login screen, so keep updates off"}
	}
	return agentCheck{Name: name, OK: true, Detail: "off"}
}

func checkHeat() agentCheck {
	const name = "heat"
	out, ok := awayCommand("pmset", "-g", "therm")
	if !ok {
		return couldNotCheck(name)
	}
	limit, known := cpuSpeedLimit(out)
	if !known || limit >= awayHotLimit {
		return agentCheck{Name: name, OK: true, Detail: "no thermal pressure"}
	}
	return agentCheck{Name: name, Detail: fmt.Sprintf("CPU held at %d%% for heat - no fix starts until it cools", limit), Fix: "give the laptop air"}
}

func checkDisk(dir string) agentCheck {
	const name = "disk"
	free, ok := utils.FreeDiskBytes(dir)
	if !ok {
		return couldNotCheck(name)
	}
	freeGB := int(free / (1 << 30))
	if freeGB >= awayDiskMinGB {
		return agentCheck{Name: name, OK: true, Detail: fmt.Sprintf("%d GB free", freeGB)}
	}
	return agentCheck{Name: name, Detail: fmt.Sprintf("%d GB free - worktrees and images fill it", freeGB),
		Fix: "corgi agent watch prune, docker system prune"}
}

func checkNetwork() agentCheck {
	const name = "network"
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(captiveProbeURL)
	if err != nil {
		return agentCheck{Name: name, Detail: "offline: " + err.Error(), Fix: "check the Wi-Fi"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode == http.StatusOK && captiveOK(string(body)) {
		return agentCheck{Name: name, OK: true, Detail: "open internet"}
	}
	return agentCheck{Name: name, Detail: "a captive portal is in the way (a Wi-Fi login page)",
		Fix: "log in on the portal; hotels often ask again every day - a travel router or a hotspot avoids that"}
}

func checkPulse(dir string) agentCheck {
	const name = "pulse"
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil || user.PulseUrl == "" {
		return agentCheck{Name: name, Detail: "nobody is told when this machine goes quiet",
			Fix: "corgi agent pulse <url> - a healthchecks.io or Uptime Kuma push URL"}
	}
	last := daemon.ReadPulse(dir)
	switch {
	case last.Error != "":
		return agentCheck{Name: name, Detail: "last ping failed: " + last.Error, Fix: "check the URL and the network"}
	case last.At.IsZero():
		return agentCheck{Name: name, OK: true, Detail: "set - no ping yet (corgi agent restart)"}
	}
	return agentCheck{Name: name, OK: true, Detail: "last ping " + time.Since(last.At).Round(time.Second).String() + " ago"}
}

func checkTunnel(dir string) agentCheck {
	const name = "tunnel"
	up := loadUpSettings(dir)
	if up.Provider == "" {
		return agentCheck{Name: name, Detail: "no tunnel - the phone cannot reach this machine from outside", Fix: "corgi agent up"}
	}
	detail := up.Provider
	if up.Provider == "ngrok" {
		detail += " - the free plan caps requests per month; a cloudflared tunnel has no cap"
	}
	return agentCheck{Name: name, OK: true, Detail: detail}
}

func checkPhone(dir string) agentCheck {
	const name = "phone"
	if n := len(push.Load(dir).List()); n > 0 {
		return agentCheck{Name: name, OK: true, Detail: fmt.Sprintf("%d device(s) get pushes", n)}
	}
	return agentCheck{Name: name, Detail: "no phone is paired for pushes", Fix: "corgi agent up, then scan the QR with the app"}
}

func digestChecks(user *config.UserConfig) []agentCheck {
	var checks []agentCheck
	for _, id := range sortedWorkspaceIDs(user) {
		wc := user.Workspaces[id]
		if wc.Watch == nil || !wc.Watch.Enabled {
			continue
		}
		c := agentCheck{Name: "digest · " + id, OK: true, Detail: "a daily digest reaches the phone"}
		if !hasRoutine(wc.Routines, "digest") {
			c.OK = false
			c.Detail = "no digest - nothing sums the day up for you"
			c.Fix = "corgi agent routine add digest, in " + id
		}
		checks = append(checks, c)
	}
	return checks
}

func isolationChecks(user *config.UserConfig) []agentCheck {
	var checks []agentCheck
	for _, id := range sortedWorkspaceIDs(user) {
		wc := user.Workspaces[id]
		if wc.Watch == nil || !wc.Watch.Enabled || wc.Watch.Action != "fix" {
			continue
		}
		c := agentCheck{Name: "isolate · " + id, OK: true, Detail: "fixes run in worktrees, pruned after " + wc.Watch.PruneAfter}
		switch {
		case !wc.Watch.Isolate:
			c.OK, c.Detail, c.Fix = false, "fixes run in the checkout", "corgi agent watch enable --isolate --prune-after 7d, in "+id
		case wc.Watch.PruneAfter == "":
			c.OK, c.Detail, c.Fix = false, "worktrees are never pruned", "corgi agent watch enable --prune-after 7d, in "+id
		}
		checks = append(checks, c)
	}
	return checks
}

func checkClaudeUpdater() agentCheck {
	const name = "claude updates"
	if os.Getenv("DISABLE_AUTOUPDATER") == "1" {
		return agentCheck{Name: name, OK: true, Detail: "pinned (DISABLE_AUTOUPDATER=1)"}
	}
	return agentCheck{Name: name, OK: true, Detail: "pinned for unattended runs (corgi sets DISABLE_AUTOUPDATER=1 on each); your own terminal still updates"}
}

func hasRoutine(list []config.Routine, name string) bool {
	for _, r := range list {
		if !r.Off && (strings.EqualFold(r.Name, name) || strings.EqualFold(r.Kind, name)) {
			return true
		}
	}
	return false
}

func sortedWorkspaceIDs(user *config.UserConfig) []string {
	ids := make([]string, 0, len(user.Workspaces))
	for id := range user.Workspaces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
