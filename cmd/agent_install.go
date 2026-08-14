package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// Agent mode starts at login through the platform's own supervisor. Nothing
// secret is ever written into the service file: on macOS these live in
// ~/Library/LaunchAgents, which is world-readable and lands in backups. The
// file names paths; the daemon reads credentials itself at start.

var agentInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Start agent mode at login (launchd on macOS, systemd on Linux)",
	Run:   runAgentInstall,
}

var agentUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop starting agent mode at login",
	Run:   runAgentUninstall,
}

const launchdLabel = "com.andriiklymiuk.corgi.agent"
const systemdUnitName = "corgi-agent.service"

func installSupported() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return true
	}
	return false
}

func installMechanism() string {
	switch runtime.GOOS {
	case "darwin":
		return "launchd"
	case "linux":
		return "systemd (user)"
	}
	return "unsupported"
}

func runAgentInstall(_ *cobra.Command, _ []string) {
	if !installSupported() {
		// Stated plainly rather than half-installing: silent partial support is
		// the worst option, because it looks like it worked.
		exitWithError("agent_install_unsupported",
			fmt.Errorf("agent mode start-at-login is macOS and Linux for now, not %s.\n"+
				"Run `corgi agent serve` under your own supervisor instead", runtime.GOOS), 2)
	}

	binary, err := os.Executable()
	if err != nil {
		exitWithError("agent_install", err, 1)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		exitWithError("agent_install", err, 1)
	}

	logDir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		exitWithError("agent_install", err, 1)
	}

	switch runtime.GOOS {
	case "darwin":
		installLaunchd(binary, logDir)
	case "linux":
		installSystemd(binary, logDir)
	}
}

func installLaunchd(binary, logDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		exitWithError("agent_install", err, 1)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>agent</string>
		<string>serve</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchdLabel, binary, filepath.Join(logDir, "agent.log"), filepath.Join(logDir, "agent.err.log"))

	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		exitWithError("agent_install", err, 1)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		exitWithError("agent_install", err, 1)
	}

	// bootout first so a reinstall picks up a changed binary path.
	_ = exec.Command("launchctl", "bootout", "gui/"+currentUID(), plistPath).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "gui/"+currentUID(), plistPath).CombinedOutput(); err != nil {
		exitWithError("agent_install",
			fmt.Errorf("launchctl bootstrap failed: %v\n%s", err, out), 1)
	}

	utils.Infof("installed %s\n", plistPath)
	utils.Info("corgi agent now starts at login. Check it with `corgi agent status`.")
}

func installSystemd(binary, logDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		exitWithError("agent_install", err, 1)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unitPath := filepath.Join(unitDir, systemdUnitName)

	unit := fmt.Sprintf(`[Unit]
Description=corgi agent — keeps Claude Code Remote Control running
After=network-online.target

[Service]
Type=simple
ExecStart=%s agent serve
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, binary)

	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		exitWithError("agent_install", err, 1)
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		exitWithError("agent_install", err, 1)
	}

	run := func(args ...string) {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			exitWithError("agent_install",
				fmt.Errorf("systemctl %v failed: %v\n%s", args, err, out), 1)
		}
	}
	run("--user", "daemon-reload")
	run("--user", "enable", "--now", systemdUnitName)

	utils.Infof("installed %s\n", unitPath)
	utils.Info("corgi agent now starts at login. Check it with `corgi agent status`.")
	utils.Info("If it should survive logout too: `loginctl enable-linger $USER`")
	_ = logDir
}

func runAgentUninstall(_ *cobra.Command, _ []string) {
	if !installSupported() {
		exitWithError("agent_install_unsupported",
			fmt.Errorf("nothing to uninstall on %s", runtime.GOOS), 2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		exitWithError("agent_uninstall", err, 1)
	}

	switch runtime.GOOS {
	case "darwin":
		plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
		_ = exec.Command("launchctl", "bootout", "gui/"+currentUID(), plistPath).Run()
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			exitWithError("agent_uninstall", err, 1)
		}
		utils.Infof("removed %s\n", plistPath)
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "--now", systemdUnitName).Run()
		unitPath := filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
		if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
			exitWithError("agent_uninstall", err, 1)
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		utils.Infof("removed %s\n", unitPath)
	}
	utils.Info("corgi agent no longer starts at login")
}

func currentUID() string { return fmt.Sprint(os.Getuid()) }

func init() {
	agentCmd.AddCommand(agentInstallCmd, agentUninstallCmd)
}
