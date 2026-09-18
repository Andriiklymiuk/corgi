package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

const atLoginFlag = "at-login"

func ensureAtLogin(dir string, cmd *cobra.Command, settings *upSettings) {
	if cmd != nil && cmd.Flags().Changed(atLoginFlag) {
		want, _ := cmd.Flags().GetBool(atLoginFlag)
		if !want {
			setAtLogin(dir, settings, false)
			return
		}
		enableAtLogin(dir, settings)
		return
	}

	if settings.AtLogin {
		if !loginServiceInstalled() {
			enableAtLogin(dir, settings)
		}
		return
	}

	switch {
	case !installSupported():
		return
	case settings.AtLoginAsked:
		return
	case utils.NonInteractive || utils.JSONOutput:
		hintAtLogin()
		return
	}
	if !confirmAtLogin() {
		setAtLogin(dir, settings, false)
		return
	}
	enableAtLogin(dir, settings)
}

func enableAtLogin(dir string, settings *upSettings) {
	if !installSupported() {
		utils.Infof("⚠ %v\n", unsupportedInstallError())
		return
	}
	if !loginServiceInstalled() || !settings.AtLogin {
		if err := installLoginService(); err != nil {
			utils.Infof("⚠ could not set corgi to start at login: %v\n", err)
			return
		}
	}
	setAtLogin(dir, settings, true)
	utils.Info("✓ starts at login — after a reboot the daemon, the MCP endpoint and this tunnel come back on their own")
	startMenuBarIfInstalled(func(s string) { utils.Info(s) })
	if !stayAwakeEnabled(dir) {
		utils.Info("  it still sleeps between sessions though — `corgi agent awake on` keeps it reachable")
	}
}

func setAtLogin(dir string, settings *upSettings, on bool) {
	settings.AtLogin, settings.AtLoginAsked = on, true
	_ = saveUpSettings(dir, *settings)
}

func hintAtLogin() {
	if !installSupported() {
		return
	}
	utils.Info("↻ this does not survive a reboot yet — `corgi agent up --at-login` once, and it comes back on its own")
}

func confirmAtLogin() bool {
	p := promptui.Prompt{
		Label:     "Start corgi agent at login, and bring this endpoint back after a reboot",
		IsConfirm: true,
		Default:   "y",
	}
	_, err := p.Run()
	return err == nil
}

func awaitMCPBound(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if mcpListening(addr) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func restoreUpAtLogin(dir string) {
	settings := loadUpSettings(dir)
	if !settings.AtLogin {
		return
	}
	addr := settings.HTTP
	if addr == "" {
		addr = defaultMCPAddr
	}
	release, err := acquireUpLock(dir)
	if err != nil {
		return
	}
	defer release()

	if mcpListening(addr) {
		if !stopStaleMCP(dir, addr) {
			return
		}
	}
	tunnelFlags, err := tunnelArgs(settings.Provider, settings.TunnelName, settings.TunnelHostname)
	if err != nil {
		utils.Infof("agent: not restoring the tunnel — %v\n", err)
		return
	}
	if err := spawnDetachedMCP(dir, addr, tunnelFlags); err != nil {
		utils.Infof("agent: could not restore the MCP endpoint — %v\n", err)
		return
	}
	utils.Infof("agent: restoring the MCP endpoint on %s from your last `corgi agent up`\n", addr)
	awaitMCPBound(addr, 15*time.Second)
}

func stopStaleMCP(dir, addr string) bool {
	spawnedBy := ""
	if data, err := os.ReadFile(filepath.Join(dir, mcpVersionName)); err == nil {
		spawnedBy = strings.TrimSpace(string(data))
	}
	if spawnedBy == APP_VERSION {
		return false
	}
	pid, ok := readAgentPidFile(filepath.Join(dir, mcpPidName))
	if !ok || !utils.PidAlive(pid, "") {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil || proc.Signal(syscall.SIGTERM) != nil {
		return false
	}
	was := spawnedBy
	if was == "" {
		was = "an older corgi"
	}
	utils.Infof("agent: restarting the MCP endpoint — it was started by %s, this is %s\n", was, APP_VERSION)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !mcpListening(addr) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
