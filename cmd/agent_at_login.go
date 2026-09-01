package cmd

import (
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// Start-at-login has two halves, and only one of them existed before:
// `corgi agent install` brings the DAEMON back after a reboot, but everything
// `corgi agent up` starts — the MCP endpoint, the tunnel, the pairing server —
// died with the old session and had to be typed again. atLogin ties the two
// together: `up --at-login` installs the service AND records that the daemon
// should repeat this up when it next starts itself.
//
// It stays opt-in on purpose. A public tunnel must never appear at login
// because a command that never mentioned one was run once.

const atLoginFlag = "at-login"

// ensureAtLogin settles start-at-login for this `up`, mutating and saving
// settings. Called once the daemon is up, so it runs on every `up` path,
// including the ones that find the MCP already listening.
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
			// The flag says restore, but the service file is gone — someone ran
			// `agent uninstall`, or a migration lost it. Put it back rather
			// than promising a restore that cannot happen.
			enableAtLogin(dir, settings)
		}
		return
	}

	switch {
	case !installSupported():
		return
	case settings.AtLoginAsked:
		// Answered once already. A daily `up` must neither re-ask nor nag about
		// a decision the user has made; `--at-login` turns it on later.
		return
	case utils.NonInteractive || utils.JSONOutput:
		// Nobody to ask: an agent or a script gets the one-liner instead.
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
	// Coming back at login is only half of being reachable: a sleeping laptop
	// answers nothing, and between sessions nothing holds it awake.
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

// restoreUpAtLogin is the other half: the daemon, starting at login, brings
// back the MCP endpoint and tunnel the last `up --at-login` used. Silent and
// best-effort — a daemon must never fail to supervise workspaces because a
// tunnel provider was not reachable yet.
// awaitMCPBound waits for the freshly spawned server to take the port. A
// timeout is not an error: the server logs its own failure, and the caller has
// nothing useful left to do about it.
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
	// The same lock `agent up` takes: when the daemon was started BY an `up`,
	// that up is about to start the MCP itself, and two would race for the port.
	release, err := acquireUpLock(dir)
	if err != nil {
		return
	}
	defer release()

	if mcpListening(addr) {
		return
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
	// Keep the lock until the port is actually bound. Releasing at spawn leaves
	// a window where an `agent up` seconds later still sees a free port and
	// starts a second server, and the two fight over it.
	awaitMCPBound(addr, 15*time.Second)
}
