package cmd

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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

// systemctlUser scopes systemctl to the calling user's manager.
const systemctlUser = "--user"

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
	// Deliberately NOT EvalSymlinks. Homebrew installs corgi as a symlink into
	// a versioned Cellar directory; resolving it would bake that version into
	// the service file, and the next `brew upgrade corgi` would delete the path
	// and agent mode would silently stop starting at login.

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

// servicePATH is the PATH the supervised daemon runs with.
//
// launchd and systemd start services with a minimal PATH that does not include
// Homebrew or ~/.local/bin, so `claude` would not be found: five startup
// failures, the workspace disabled, and `corgi agent doctor` in the user's own
// shell passing all the while. The installing shell's PATH is captured, since
// that is the one where the user verified their setup, with the usual locations
// added in case corgi was invoked from somewhere unusual.
// serviceEnv is the environment the supervised daemon needs to resolve the same
// state the installing shell did.
//
// PATH alone is not enough: getDataPath keys on CORGI_DATA_DIR, HOMEBREW_PREFIX
// and XDG_DATA_HOME, so a custom Homebrew prefix or a shell-set XDG_DATA_HOME
// would leave the daemon reading a different, empty registry than the shell
// that ran `corgi agent init` — the same skew capturing PATH exists to prevent.
func serviceEnv() map[string]string {
	env := map[string]string{"PATH": servicePATH()}
	for _, key := range []string{"CORGI_DATA_DIR", "HOMEBREW_PREFIX", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			env[key] = v
		}
	}
	return env
}

// sortedEnv returns the environment as stable key/value pairs, so a reinstall
// produces an identical file.
func sortedEnv(env map[string]string) [][2]string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, env[k]})
	}
	return out
}

func servicePATH() string {
	seen := map[string]bool{}
	var parts []string
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, "bin"))
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		add(dir)
	}
	return strings.Join(parts, string(filepath.ListSeparator))
}

func installLaunchd(binary, logDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		exitWithError("agent_install", err, 1)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")

	plist := renderedLaunchdPlist(binary, filepath.Join(logDir, "agent.log"), filepath.Join(logDir, "agent.err.log"), serviceEnv())

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

// renderedLaunchdPlist is the plist corgi installs. It deliberately contains no
// credential material: files in ~/Library/LaunchAgents are world-readable and
// land in backups, so the daemon reads its own config at start instead.
func renderedLaunchdPlist(binary, outLog, errLog string, env map[string]string) string {
	// A path may contain & or <, which would produce an invalid plist and an
	// opaque `launchctl bootstrap` failure.
	binary, outLog, errLog = escapeXML(binary), escapeXML(outLog), escapeXML(errLog)

	var envEntries strings.Builder
	for _, kv := range sortedEnv(env) {
		fmt.Fprintf(&envEntries, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n",
			escapeXML(kv[0]), escapeXML(kv[1]))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
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
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
	<key>RunAtLoad</key>
	<true/>
	<!-- Restart only on an abnormal end. corgi decides for itself when a
	     workspace should stay down (auth failure, repeated crashes), and
	     SuccessfulExit=false would restart precisely those cases in a loop. -->
	<key>KeepAlive</key>
	<dict>
		<key>Crashed</key>
		<true/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchdLabel, binary, envEntries.String(), outLog, errLog)
}

func installSystemd(binary, logDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		exitWithError("agent_install", err, 1)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unitPath := filepath.Join(unitDir, systemdUnitName)

	unit := renderedSystemdUnit(binary, serviceEnv())

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
	run(systemctlUser, "daemon-reload")
	run(systemctlUser, "enable", "--now", systemdUnitName)

	utils.Infof("installed %s\n", unitPath)
	utils.Info("corgi agent now starts at login. Check it with `corgi agent status`.")
	utils.Info("If it should survive logout too: `loginctl enable-linger $USER`")
	_ = logDir
}

// renderedSystemdUnit is the user unit corgi installs. Like the plist, it
// carries no credential material.
func renderedSystemdUnit(binary string, env map[string]string) string {
	var envLines strings.Builder
	for _, kv := range sortedEnv(env) {
		// Quoted: a value containing a space would otherwise truncate there.
		fmt.Fprintf(&envLines, "Environment=\"%s=%s\"\n", kv[0], kv[1])
	}
	return fmt.Sprintf(`[Unit]
Description=corgi agent — keeps Claude Code Remote Control running
After=network-online.target

[Service]
Type=simple
%sExecStart=%s agent serve
# corgi decides for itself when to stay down: an auth failure or a bad config
# exits non-zero on purpose, so restarting on any failure would loop on exactly
# the cases the supervisor deliberately gave up on.
Restart=on-abnormal
RestartSec=5

[Install]
WantedBy=default.target
`, envLines.String(), binary)
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
		_ = exec.Command("systemctl", systemctlUser, "disable", "--now", systemdUnitName).Run()
		unitPath := filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
		if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
			exitWithError("agent_uninstall", err, 1)
		}
		_ = exec.Command("systemctl", systemctlUser, "daemon-reload").Run()
		utils.Infof("removed %s\n", unitPath)
	}
	utils.Info("corgi agent no longer starts at login")
}

// escapeXML makes a path safe to interpolate into the plist.
func escapeXML(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

func currentUID() string { return fmt.Sprint(os.Getuid()) }

func init() {
	agentCmd.AddCommand(agentInstallCmd, agentUninstallCmd)
}
