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

const systemctlUser = "--user"

var runSupervisorCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

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

func unsupportedInstallError() error {
	return fmt.Errorf("agent mode start-at-login is macOS and Linux for now, not %s.\n"+
		"Run `corgi agent serve` under your own supervisor instead", runtime.GOOS)
}

func loginServicePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
	}
	return ""
}

func loginServiceInstalled() bool {
	path := loginServicePath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func runAgentInstall(_ *cobra.Command, _ []string) {
	if !installSupported() {
		exitWithError("agent_install_unsupported", unsupportedInstallError(), 2)
	}
	if err := installLoginService(); err != nil {
		exitWithError("agent_install", err, 1)
	}
	reportLoginInstall(adoptSavedUpAtLogin())
}

func reportLoginInstall(withUp bool) {
	utils.Info("corgi agent now starts at login. Check it with `corgi agent status`.")
	if withUp {
		utils.Info("it also restores the MCP endpoint and tunnel from your last `corgi agent up`")
		return
	}
	utils.Info("that is the daemon only — run `corgi agent up --at-login` in a stack to also restore the tunnel and pairing server")
}

func adoptSavedUpAtLogin() bool {
	dir, err := agentDir()
	if err != nil || !upSettingsExist(dir) {
		return false
	}
	s := loadUpSettings(dir)
	if s.AtLogin {
		return true
	}
	s.AtLogin, s.AtLoginAsked = true, true
	return saveUpSettings(dir, s) == nil
}

func installLoginService() error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	// No EvalSymlinks on Linux: Homebrew's versioned path would break at the next upgrade.
	if daemonRunsFromStableCopy() {
		if binary, err = refreshStableDaemonBinary(binary); err != nil {
			return fmt.Errorf("copy corgi for the daemon: %w", err)
		}
	}

	logDir, err := agentDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(binary, logDir)
	case "linux":
		return installSystemd(binary, logDir)
	}
	return unsupportedInstallError()
}

func serviceEnv() map[string]string {
	env := map[string]string{"PATH": servicePATH()}
	for _, key := range []string{"CORGI_DATA_DIR", "HOME", "XDG_DATA_HOME", "HOMEBREW_PREFIX", "TZ"} {
		if v := os.Getenv(key); v != "" {
			env[key] = v
		}
	}
	return env
}

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

func installLaunchd(binary, logDir string) error {
	plistPath := loginServicePath()
	if plistPath == "" {
		return fmt.Errorf("could not resolve your home directory")
	}

	plist := renderedLaunchdPlist(binary, filepath.Join(logDir, "agent.log"), filepath.Join(logDir, "agent.err.log"), serviceEnv())

	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	_, _ = runSupervisorCommand("launchctl", "bootout", "gui/"+currentUID(), plistPath)
	if out, err := runSupervisorCommand("launchctl", "bootstrap", "gui/"+currentUID(), plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %v\n%s", err, out)
	}

	utils.Infof("installed %s\n", plistPath)
	return nil
}

func renderedLaunchdPlist(binary, outLog, errLog string, env map[string]string) string {
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

func installSystemd(binary, logDir string) error {
	unitPath := loginServicePath()
	if unitPath == "" {
		return fmt.Errorf("could not resolve your home directory")
	}

	unit := renderedSystemdUnit(binary, serviceEnv())

	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}

	run := func(args ...string) error {
		if out, err := runSupervisorCommand("systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %v failed: %v\n%s", args, err, out)
		}
		return nil
	}
	if err := run(systemctlUser, "daemon-reload"); err != nil {
		return err
	}
	if err := run(systemctlUser, "enable", "--now", systemdUnitName); err != nil {
		return err
	}

	utils.Infof("installed %s\n", unitPath)
	if on, known := lingerEnabled(); known && !on {
		utils.Info("it stops when your last login ends — `loginctl enable-linger $USER` keeps it up on a server")
	} else if !known {
		utils.Info("If it should survive logout too: `loginctl enable-linger $USER`")
	}
	_ = logDir
	return nil
}

func lingerEnabled() (on, known bool) {
	if runtime.GOOS != "linux" {
		return false, false
	}
	out, err := runSupervisorCommand("loginctl", "show-user", currentUID(), "-p", "Linger", "--value")
	if err != nil {
		return false, false
	}
	return lingerFromOutput(out), true
}

func lingerFromOutput(out []byte) bool {
	return strings.TrimSpace(string(out)) == "yes"
}

func renderedSystemdUnit(binary string, env map[string]string) string {
	var envLines strings.Builder
	for _, kv := range sortedEnv(env) {
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
	path := loginServicePath()

	switch runtime.GOOS {
	case "darwin":
		_, _ = runSupervisorCommand("launchctl", "bootout", "gui/"+currentUID(), path)
	case "linux":
		_, _ = runSupervisorCommand("systemctl", systemctlUser, "disable", "--now", systemdUnitName)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		exitWithError("agent_uninstall", err, 1)
	}
	if runtime.GOOS == "linux" {
		_, _ = runSupervisorCommand("systemctl", systemctlUser, "daemon-reload")
	}
	utils.Infof("removed %s\n", path)

	clearAtLogin()
	utils.Info("corgi agent no longer starts at login")
}

func clearAtLogin() {
	dir, err := agentDir()
	if err != nil || !upSettingsExist(dir) {
		return
	}
	s := loadUpSettings(dir)
	if !s.AtLogin {
		return
	}
	s.AtLogin = false
	_ = saveUpSettings(dir, s)
}

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
