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

// runSupervisorCommand runs launchctl / systemctl. A seam, so the file-writing and
// error paths can be tested without loading a real job into the test runner's
// login session.
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

// loginServicePath is where this platform keeps corgi's start-at-login file.
// Empty on a platform with no supported mechanism.
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

// loginServiceInstalled reports whether the start-at-login file is in place.
// The file, not the running process: the question it answers is "will this come
// back after a reboot", which a currently-running daemon does not settle.
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
		// Stated plainly rather than half-installing: silent partial support is
		// the worst option, because it looks like it worked.
		exitWithError("agent_install_unsupported", unsupportedInstallError(), 2)
	}
	if err := installLoginService(); err != nil {
		exitWithError("agent_install", err, 1)
	}
	reportLoginInstall(adoptSavedUpAtLogin())
}

// reportLoginInstall says what login start will actually bring back, which is
// the daemon alone unless a previous `agent up` left settings to repeat.
func reportLoginInstall(withUp bool) {
	utils.Info("corgi agent now starts at login. Check it with `corgi agent status`.")
	if withUp {
		utils.Info("it also restores the MCP endpoint and tunnel from your last `corgi agent up`")
		return
	}
	utils.Info("that is the daemon only — run `corgi agent up --at-login` in a stack to also restore the tunnel and pairing server")
}

// adoptSavedUpAtLogin turns on tunnel restore when a previous `agent up` left
// settings to repeat. Nothing is invented for a user who never ran `up`: a
// public tunnel must never start from a command that did not ask for one.
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

// installLoginService writes the platform's service file and loads it.
func installLoginService() error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	// Deliberately NOT EvalSymlinks. Homebrew installs corgi as a symlink into
	// a versioned Cellar directory; resolving it would bake that version into
	// the service file, and the next `brew upgrade corgi` would delete the path
	// and agent mode would silently stop starting at login.

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

// servicePATH is the PATH the supervised daemon runs with. launchd and systemd
// start services with a minimal PATH that would not find `claude`, so the
// installing shell's PATH is captured plus the usual locations.
// serviceEnv keeps the daemon reading the same agent dir as the installing
// shell. NativeDataDir keys on CORGI_DATA_DIR, HOME and XDG_DATA_HOME, so PATH
// alone is not enough. HOMEBREW_PREFIX is for the legacy exec-path registry.
func serviceEnv() map[string]string {
	env := map[string]string{"PATH": servicePATH()}
	// HOME is normally injected by launchd/systemd, but the daemon's data-dir
	// resolution now depends on it, so pin it rather than rely on the launcher.
	for _, key := range []string{"CORGI_DATA_DIR", "HOME", "XDG_DATA_HOME", "HOMEBREW_PREFIX"} {
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

	// bootout first so a reinstall picks up a changed binary path.
	_, _ = runSupervisorCommand("launchctl", "bootout", "gui/"+currentUID(), plistPath)
	if out, err := runSupervisorCommand("launchctl", "bootstrap", "gui/"+currentUID(), plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %v\n%s", err, out)
	}

	utils.Infof("installed %s\n", plistPath)
	return nil
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
	utils.Info("If it should survive logout too: `loginctl enable-linger $USER`")
	_ = logDir
	return nil
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

	// The service file is gone, so nothing would read the flag anyway; clearing
	// it keeps `agent status` from claiming a restore that cannot happen.
	clearAtLogin()
	utils.Info("corgi agent no longer starts at login")
}

// clearAtLogin turns off tunnel restore, leaving the rest of the saved `up`
// settings alone so the next `agent up` still repeats the named tunnel.
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
