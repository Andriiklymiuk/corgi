package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var agentInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Opt this stack into agent mode",
	Long: `Registers the current directory as an agent-mode workspace and writes
.corgi/agent.yml.

That file is committed and holds identity only — id and aliases. Anything that
grants capability (which binary runs, which Claude config directory, permission
mode) lives in the user-level config instead, because a committed file arrives
with a clone and is not written by whoever runs the daemon.`,
	Run: runAgentInit,
}

func runAgentInit(cmd *cobra.Command, _ []string) {
	cwd, err := os.Getwd()
	if err != nil {
		exitWithError("agent_cwd", err, 1)
	}
	id, _ := cmd.Flags().GetString("id")
	aliases, _ := cmd.Flags().GetStringSlice("alias")
	configDir, _ := cmd.Flags().GetString("config-dir")
	sensitive, _ := cmd.Flags().GetBool("sensitive")
	skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")

	id, err = registerWorkspace(cwd, id, aliases, configDir, sensitive, skipPerms)
	if err != nil {
		var re *registerError
		if errors.As(err, &re) {
			exitWithError(re.code, re.err, re.exit)
		}
		exitWithError("agent_init", err, 1)
	}

	utils.Infof("registered %s (%s) and enabled it\n", id, cwd)
	utils.Info("wrote .corgi/agent.yml — safe to commit, it holds identity only")
	if skipPerms {
		utils.Info("⚠ permissions: SKIPPED for this workspace — its remote sessions run without the prompts you answer from your phone.")
		utils.Infof("  to undo: remove `dangerouslySkipPermissions: true` for %s from %s\n", id, agentUserConfigPath(mustAgentDir()))
	}
	if configDir == "" {
		utils.Info("no config dir set: this workspace uses your default Claude account.")
		utils.Info("If you keep work and personal logins separate, set one:")
		utils.Infof("  corgi agent init --config-dir ~/.claude-work\n")
	}
	warnIfUntrusted(configDir, cwd)
	utils.Info("next: `corgi agent install` to start at login, then `corgi agent status`")
}

type registerError struct {
	code string
	exit int
	err  error
}

func (e *registerError) Error() string { return e.err.Error() }
func (e *registerError) Unwrap() error { return e.err }

func registerWorkspace(dir, id string, aliases []string, configDir string, sensitive, skipPerms bool) (string, error) {
	if !dirIsWorkspace(dir) {
		return "", &registerError{"agent_no_workspace", 2,
			fmt.Errorf("nothing to register here — run this in a corgi stack or a git repository (or `corgi agent scan <dir>` to find stacks)")}
	}
	if id == "" {
		id = filepath.Base(dir)
	}

	registry, path, err := agentRegistry()
	if err != nil {
		return "", &registerError{"agent_registry_read", 1, err}
	}
	if prior, ok := registry.Find(id); ok && prior.AbsPath != "" && prior.AbsPath != dir {
		return "", &registerError{"agent_id_taken", 2, fmt.Errorf(
			"workspace id %q already belongs to %s — its settings (account, permissions) must not transfer here. "+
				"Pass --id <something-else> to register this directory under its own name",
			id, prior.AbsPath)}
	}

	if err := writeRepoAgentConfig(dir, id, aliases, sensitive); err != nil {
		return "", &registerError{"agent_write_repo_config", 1, err}
	}

	existing, _ := registry.Find(id)
	existing.ID = id
	existing.AbsPath = dir
	existing.ComposeFile = registeredComposeFile(dir)
	existing.Aliases = aliases
	existing.Status = workspace.StatusOK
	existing.Services, existing.Repos = describeStack(dir)
	registry.Upsert(existing)
	if err := workspace.Save(path, registry); err != nil {
		return "", &registerError{"agent_registry_write", 1, err}
	}

	if err := enableWorkspace(id, configDir, skipPerms); err != nil {
		return "", &registerError{"agent_write_user_config", 1, err}
	}
	return id, nil
}

func describeStack(dir string) (services, repos []string) {
	data, err := os.ReadFile(filepath.Join(dir, composeFileName(dir)))
	if err != nil {
		return nil, nil
	}
	var doc struct {
		Services map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, nil
	}

	seenRepo := map[string]bool{}
	for name, svc := range doc.Services {
		services = append(services, name)
		if svc.Path == "" {
			continue
		}
		abs := svc.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		if root, ok := utils.RepoRootOf(abs); ok {
			repoName := filepath.Base(root)
			if !seenRepo[repoName] {
				seenRepo[repoName] = true
				repos = append(repos, repoName)
			}
		}
	}
	sort.Strings(services)
	sort.Strings(repos)
	return services, repos
}

func composeFileName(dir string) string {
	for _, name := range []string{"corgi-compose.yml", "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name
		}
	}
	return "corgi-compose.yml"
}

func claudeTrustsDir(configDir, dir string) bool {
	base := expandTilde(configDir)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return true
		}
		base = home
	}
	data, err := os.ReadFile(filepath.Join(base, ".claude.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Projects == nil {
		return true
	}
	return cfg.Projects[dir].HasTrustDialogAccepted
}

func warnIfUntrusted(configDir, dir string) {
	if claudeTrustsDir(configDir, dir) {
		return
	}
	utils.Infof("⚠ Claude has not trusted %s yet%s — run `claude` there once and accept the trust dialog, or the remote session will fail to start.\n",
		dir, trustAccountSuffix(configDir))
}

func trustAccountSuffix(configDir string) string {
	if configDir == "" {
		return ""
	}
	return " under " + configDir
}

func registeredComposeFile(dir string) string {
	if dirHasComposeFile(dir) {
		return composeFileName(dir)
	}
	return ""
}

func writeRepoAgentConfig(dir, id string, aliases []string, sensitive bool) error {
	cfg := config.RepoConfig{
		Version: 1,
		Workspace: config.RepoWorkspace{
			ID:        id,
			Aliases:   aliases,
			Sensitive: sensitive,
		},
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := []byte("# corgi agent mode — committed, identity only.\n" +
		"# Capability settings (bin, configDir, permissionMode) live in the\n" +
		"# user-level config, never here: this file arrives with a clone.\n")

	target := filepath.Join(dir, ".corgi")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, "agent.yml"), append(header, body...), 0o644)
}

func setWorkspaceAutostart(id string, on bool) error {
	dir, err := agentDir()
	if err != nil {
		return err
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	entry := user.Workspaces[id]
	entry.Autostart = &on
	user.Workspaces[id] = entry
	return writeUserConfig(path, user)
}

func enableWorkspace(id, configDir string, skipPerms bool) error {
	dir, err := agentDir()
	if err != nil {
		return err
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	entry := user.Workspaces[id]
	on := true
	entry.Autostart = &on
	if configDir != "" {
		entry.ConfigDir = configDir
	}
	if skipPerms {
		entry.DangerouslySkipPermissions = true
	}
	user.Workspaces[id] = entry

	return writeUserConfig(path, user)
}

var agentScanCmd = &cobra.Command{
	Use:   "scan <dir>",
	Short: "Find corgi stacks under a directory and register them",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentScan,
}

const scanMaxDepth = 4

func runAgentScan(cmd *cobra.Command, args []string) {
	root, err := filepath.Abs(args[0])
	if err != nil {
		exitWithError("agent_bad_path", err, 2)
	}
	found := findComposeDirs(root)
	if len(found) == 0 {
		utils.Infof("no corgi-compose.yml found under %s\n", root)
		return
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	registry, path := mustLoadRegistry()

	added := 0
	for _, dir := range found {
		id := filepath.Base(dir)
		if _, exists := registry.Find(id); exists {
			continue
		}
		if dryRun {
			utils.Infof("would register %-20s %s\n", id, dir)
			continue
		}
		services, repos := describeStack(dir)
		registry.Upsert(workspace.Workspace{
			ID:          id,
			AbsPath:     dir,
			ComposeFile: composeFileName(dir),
			Services:    services,
			Repos:       repos,
			Status:      workspace.StatusOK,
		})
		added++
		utils.Infof("registered %-20s %s\n", id, dir)
	}

	if dryRun {
		return
	}
	if added == 0 {
		utils.Info("nothing new to register")
		return
	}
	if err := workspace.Save(path, registry); err != nil {
		exitWithError("agent_registry_write", err, 1)
	}
	utils.Infof("registered %d workspace(s)\n", added)
	utils.Info("none of them are supervised yet — run `corgi agent init` in the ones you want running")
}

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "target": true, "Pods": true,
	"corgi_services": true, ".venv": true, "__pycache__": true,
}

func findComposeDirs(root string) []string {
	var out []string
	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if strings.Count(path, string(filepath.Separator))-rootDepth > scanMaxDepth {
			return filepath.SkipDir
		}
		if dirHasComposeFile(path) {
			out = append(out, path)
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

var agentDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check whether agent mode can actually work here",
	Run:   runAgentDoctor,
}

const (
	checkWakeLock   = "wake lock"
	checkAtLogin    = "start at login"
	checkUserConfig = "user config"
)

type agentCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func runAgentDoctor(cmd *cobra.Command, _ []string) {
	var checks []agentCheck
	if only, _ := cmd.Flags().GetBool("security"); only {
		checks = checkSecurity()
	} else {
		checks = append(collectAgentChecks(), checkSecurity()...)
	}

	if utils.JSONOutput {
		utils.PrintJSON(checks)
		if anyCheckFailed(checks) {
			exitProcess(1)
		}
		return
	}

	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		fmt.Printf("%s %-24s %s\n", mark, c.Name, c.Detail)
		if !c.OK && c.Fix != "" {
			fmt.Printf("  %-24s → %s\n", "", c.Fix)
		}
	}
	if anyCheckFailed(checks) {
		exitProcess(1)
	}
}

func anyCheckFailed(checks []agentCheck) bool {
	for _, c := range checks {
		if !c.OK {
			return true
		}
	}
	return false
}

func collectAgentChecks() []agentCheck {
	var checks []agentCheck

	checks = append(checks, checkClaudeBinary(), checkAmbientAPIKey(), checkWakeLockSupport(), checkInstallSupport(), checkDaemonBinaryPath(), checkMenuBarApp())
	if runtime.GOOS == "linux" {
		checks = append(checks, checkNotifier())
	}

	dir, err := agentDir()
	if err != nil {
		return append(checks, agentCheck{Name: "data directory", Detail: err.Error()})
	}
	checks = append(checks, checkUserConfigPermissions(agentUserConfigPath(dir)), checkRegisteredWorkspaces(), checkDaemonRunning(dir))
	if c, ok := checkMacOSFileAccess(); ok {
		checks = append(checks, c)
	}
	checks = append(checks, checkWorkspaceTrust()...)
	checks = append(checks, checkSessionTracking(dir)...)
	checks = append(checks, checkUnattended(dir)...)
	return checks
}

func checkSessionTracking(dir string) []agentCheck {
	var checks []agentCheck
	hooked, stale := 0, 0
	dirs := trackConfigDirs(dir, nil)
	for _, cfgDir := range dirs {
		path := claudeUserSettingsPath(cfgDir)
		if !hasTrackingHooks(path) {
			continue
		}
		hooked++
		if trackingHooksStale(path) {
			stale++
		}
	}
	c := agentCheck{Name: "session tracking", OK: true, Detail: fmt.Sprintf("hooks in %d of %d Claude config dir(s)", hooked, len(dirs))}
	if hooked == 0 {
		c.Detail = "off — `corgi agent track enable` for a Stream Deck or `corgi agent sessions`"
		return append(checks, c)
	}
	if hooked < len(dirs) {
		c.Fix = "`corgi agent track enable` covers every profile's config dir"
	}
	if stale > 0 {
		c.OK = false
		c.Detail = fmt.Sprintf("hooks in %d of %d Claude config dir(s) — %d from an older corgi", hooked, len(dirs), stale)
		c.Fix = "`corgi agent track enable` again: this version adds hooks the old set does not have"
	}
	checks = append(checks, c)
	rep, err := readBoard(dir)
	if err != nil {
		return append(checks, agentCheck{Name: "session board", Detail: err.Error()})
	}
	unknown := 0
	for _, s := range rep.Sessions {
		if s.Host.Kind == sessions.HostUnknown && s.Status != sessions.StatusGone {
			unknown++
		}
	}
	board := agentCheck{Name: "session board", OK: true,
		Detail: fmt.Sprintf("%d session(s), %d window(s) connected", len(rep.Sessions), len(rep.Windows))}
	if unknown > 0 {
		board.OK = false
		board.Detail += fmt.Sprintf(", %d with no known window", unknown)
		board.Fix = "focus reaches those at app level only — reopen the terminal after installing the corgi VS Code extension, or run claude inside tmux (any OS)"
	}
	return append(checks, board)
}

func checkWorkspaceTrust() []agentCheck {
	registry, _, err := agentRegistry()
	if err != nil {
		return nil
	}
	var checks []agentCheck
	for _, ws := range registry.Sorted() {
		absPath, configDir, ok := workspaceSessionTarget(ws.ID, runningProfile(ws.ID))
		if !ok || absPath == "" {
			continue
		}
		c := agentCheck{Name: "trust · " + ws.ID, OK: true, Detail: "Claude trusts " + absPath + trustAccountSuffix(configDir)}
		if !claudeTrustsDir(configDir, absPath) {
			c.OK = false
			c.Detail = "Claude has not trusted " + absPath + trustAccountSuffix(configDir) + " — remote sessions will refuse to start"
			c.Fix = "run `claude` in that directory once and accept the trust prompt"
		}
		checks = append(checks, c)
	}
	return checks
}

func checkClaudeBinary() agentCheck {
	path, err := exec.LookPath("claude")
	if err != nil {
		return agentCheck{
			Name:   "claude binary",
			Detail: "not found on PATH",
			Fix:    "install Claude Code, then run `claude` once in a project to accept the trust dialog",
		}
	}
	return agentCheck{Name: "claude binary", OK: true, Detail: path}
}

func checkAmbientAPIKey() agentCheck {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return agentCheck{Name: "ambient credentials", OK: true, Detail: "none set"}
	}
	return agentCheck{
		Name:   "ambient credentials",
		OK:     true,
		Detail: "ANTHROPIC_API_KEY is set — corgi strips it from supervised processes",
		Fix:    "remote control requires subscription auth and refuses to start with an API key set; corgi removes it for you",
	}
}

func checkWakeLockSupport() agentCheck {
	if !supervisor.Supported() {
		return agentCheck{
			Name:   checkWakeLock,
			Detail: "unsupported on " + runtime.GOOS,
			Fix:    "sessions will end if the machine sleeps",
		}
	}
	argv := supervisor.WakeLockCommand(os.Getpid())
	if _, err := exec.LookPath(argv[0]); err != nil {
		return agentCheck{
			Name:   checkWakeLock,
			Detail: argv[0] + " not found",
			Fix:    "install " + argv[0] + ", or set wakeLock: off",
		}
	}
	detail := argv[0] + " · " + wakeLockScope()
	if risk := supervisor.CheckSleepRisk(); risk.AtRisk() {
		return agentCheck{Name: checkWakeLock, OK: true, Detail: detail + " — " + risk.Reason, Fix: risk.Fix}
	}
	if runtime.GOOS == "darwin" {
		detail += " — note: " + supervisor.ClamshellWarning
	}
	return agentCheck{Name: checkWakeLock, OK: true, Detail: detail}
}

func wakeLockScope() string {
	dir, err := agentDir()
	if err != nil {
		return "per session"
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil || !user.StayAwake {
		return "per session (the machine may sleep between them — `corgi agent awake on`)"
	}
	return "held for the daemon's whole life (stayAwake)"
}

func checkInstallSupport() agentCheck {
	if !installSupported() {
		return agentCheck{
			Name:   checkAtLogin,
			Detail: "not supported on " + runtime.GOOS,
			Fix:    "run `corgi agent serve` yourself, or supervise it with your own tooling",
		}
	}
	if !loginServiceInstalled() {
		return agentCheck{
			Name:   checkAtLogin,
			OK:     true,
			Detail: "not installed — nothing comes back after a reboot",
			Fix:    "`corgi agent up --at-login` in a stack, or `corgi agent install` for the daemon alone",
		}
	}
	detail := installMechanism() + " — daemon only"
	if dir, err := agentDir(); err == nil && loadUpSettings(dir).AtLogin {
		detail = installMechanism() + " — daemon, MCP endpoint and tunnel"
	}
	if on, known := lingerEnabled(); known && !on {
		return agentCheck{Name: checkAtLogin, OK: true, Detail: detail + ", stops with your last login", Fix: "`loginctl enable-linger $USER` so it outlives the SSH session"}
	}
	return agentCheck{Name: checkAtLogin, OK: true, Detail: detail}
}

func checkNotifier() agentCheck {
	_, err := exec.LookPath("notify-send")
	url := ""
	if dir, dirErr := agentDir(); dirErr == nil {
		if user, loadErr := config.LoadUser(agentUserConfigPath(dir)); loadErr == nil && user != nil {
			url = user.NotifyUrl
		}
	}
	return notifierCheck(err == nil, url)
}

func notifierCheck(haveNotifySend bool, notifyURL string) agentCheck {
	const name = "notifications"
	switch {
	case haveNotifySend:
		return agentCheck{Name: name, OK: true, Detail: "notify-send"}
	case notifyURL != "":
		return agentCheck{Name: name, OK: true, Detail: "no desktop notifier — notifyUrl carries them"}
	}
	return agentCheck{Name: name, OK: true, Detail: "no notify-send and no notifyUrl — nothing here shows a notification", Fix: "`corgi agent notify telegram --token <TOKEN>`, then `corgi agent restart`"}
}

func checkUserConfigPermissions(path string) agentCheck {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return agentCheck{Name: checkUserConfig, OK: true, Detail: "none yet (defaults apply)"}
	}
	if err != nil {
		return agentCheck{Name: checkUserConfig, Detail: err.Error()}
	}
	if mode := info.Mode().Perm(); runtime.GOOS != "windows" && mode&0o077 != 0 {
		return agentCheck{
			Name:   checkUserConfig,
			Detail: fmt.Sprintf("%s is readable by other users (mode %04o)", path, mode),
			Fix:    "chmod 600 " + path,
		}
	}
	return agentCheck{Name: checkUserConfig, OK: true, Detail: path}
}

func checkRegisteredWorkspaces() agentCheck {
	registry, _ := mustLoadRegistry()
	registry.Reconcile(dirIsWorkspace)

	var ok, unreachable int
	for _, w := range registry.Workspaces {
		if w.Status == workspace.StatusOK {
			ok++
		} else {
			unreachable++
		}
	}
	if ok == 0 {
		return agentCheck{
			Name:   "workspaces",
			Detail: "none registered",
			Fix:    "run `corgi agent init` in a stack, or `corgi agent scan ~/your-projects`",
		}
	}
	detail := fmt.Sprintf("%d ready", ok)
	if unreachable > 0 {
		detail += fmt.Sprintf(", %d unreachable", unreachable)
	}
	return agentCheck{Name: "workspaces", OK: true, Detail: detail}
}

func checkDaemonRunning(dir string) agentCheck {
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		return agentCheck{Name: "daemon", Detail: err.Error()}
	}
	if info == nil {
		return agentCheck{
			Name:   "daemon",
			Detail: "not running",
			Fix:    "`corgi agent install` to start at login, or `corgi agent serve --foreground` to try it now",
		}
	}
	return agentCheck{Name: "daemon", OK: true, Detail: fmt.Sprintf("running (pid %d)", info.PID)}
}

func init() {
	agentInitCmd.Flags().String("id", "", "Workspace id (defaults to the directory name)")
	agentInitCmd.Flags().StringSlice("alias", nil, "Extra names this workspace answers to, e.g. --alias 'recipe app'")
	agentInitCmd.Flags().String("config-dir", "", "CLAUDE_CONFIG_DIR for this workspace, so it runs under a specific Claude account")
	agentInitCmd.Flags().Bool("sensitive", false, "Never open a public tunnel for this workspace")
	agentInitCmd.Flags().Bool("dangerously-skip-permissions", false,
		"Run this workspace's sessions with permission prompts OFF (--permission-mode bypassPermissions). Removes the gate you answer from your phone — off by default.")

	agentScanCmd.Flags().Bool("dry-run", false, "Show what would be registered without changing anything")

	agentDoctorCmd.Flags().Bool("security", false, "only the security checks: deny rules, the secrets hook, permissions, isolation, tunnel exposure")
	agentCmd.AddCommand(agentInitCmd, agentScanCmd, agentDoctorCmd)
}

var gitConfigEmail = func(dir string) string {
	out, _ := exec.Command("git", "-C", dir, "config", "--get", "user.email").Output()
	return strings.TrimSpace(string(out))
}

func checkUnattended(dir string) []agentCheck {
	registry, _ := mustLoadRegistry()
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	var dirs []string
	fixing := map[string]bool{}
	for _, ws := range registry.Sorted() {
		if ws.AbsPath != "" {
			dirs = append(dirs, ws.AbsPath)
		}
		if user == nil {
			continue
		}
		if wc, ok := user.Workspaces[ws.ID]; ok && wc.Watch != nil && wc.Watch.Enabled && wc.Watch.Action == "fix" {
			fixing[claudeConfigDirOf(wc.ConfigDir)] = true
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	secrets := watch.LoadSecrets(dir)
	_, haveGH := lookPathOK("gh")
	_, haveGlab := lookPathOK("glab")
	checks := []agentCheck{
		gitIdentityCheck(gitIdentityMissing(dirs, gitConfigEmail)),
		forgeCLICheck(haveGH, haveGlab, secrets.GitHub != "", secrets.GitLab != ""),
	}
	if len(fixing) > 0 {
		var missing []string
		for configDir := range fixing {
			data, _ := os.ReadFile(filepath.Join(configDir, "plugins", "installed_plugins.json"))
			if !pluginInstalled(data, "corgi") {
				missing = append(missing, configDir)
			}
		}
		sort.Strings(missing)
		checks = append(checks, pluginCheck(missing))
	}
	return checks
}

func lookPathOK(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

func claudeConfigDirOf(configDir string) string {
	if strings.TrimSpace(configDir) != "" {
		return expandTilde(configDir)
	}
	return defaultClaudeConfigDir()
}

func gitIdentityMissing(dirs []string, email func(string) string) []string {
	var missing []string
	for _, d := range dirs {
		if email(d) == "" {
			missing = append(missing, d)
		}
	}
	return missing
}

func gitIdentityCheck(missing []string) agentCheck {
	const name = "git identity"
	if len(missing) == 0 {
		return agentCheck{Name: name, OK: true, Detail: "user.email set in every workspace"}
	}
	return agentCheck{Name: name, Detail: "no user.email in " + strings.Join(missing, ", ") + " — an unattended commit fails there",
		Fix: "git config --global user.name \"…\" && git config --global user.email \"…\""}
}

func forgeCLICheck(haveGH, haveGlab, githubToken, gitlabToken bool) agentCheck {
	const name = "forge cli"
	var have, need []string
	if haveGH {
		have = append(have, "gh")
	}
	if haveGlab {
		have = append(have, "glab")
	}
	if githubToken && !haveGH {
		need = append(need, "gh (a GitHub token is set, the pull request is opened with it): install it, then `gh auth login --with-token`")
	}
	if gitlabToken && !haveGlab {
		need = append(need, "glab (a GitLab token is set, the merge request is opened with it): install it, then `glab auth login --token …`")
	}
	if len(need) > 0 {
		return agentCheck{Name: name, Detail: "missing " + strings.Join(need, "; "), Fix: "a fix that cannot open its pull request ends with the work stuck on a branch"}
	}
	if len(have) == 0 {
		return agentCheck{Name: name, OK: true, Detail: "none — fine until a watch opens pull requests", Fix: "install gh or glab and log it in before `watch enable --action fix`"}
	}
	return agentCheck{Name: name, OK: true, Detail: strings.Join(have, ", ")}
}

func pluginInstalled(data []byte, name string) bool {
	var f struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return false
	}
	for key := range f.Plugins {
		if strings.HasPrefix(key, name+"@") {
			return true
		}
	}
	return false
}

func pluginCheck(missingIn []string) agentCheck {
	const name = "corgi plugin"
	if len(missingIn) == 0 {
		return agentCheck{Name: name, OK: true, Detail: "installed for every account that fixes"}
	}
	return agentCheck{Name: name, Detail: "not installed under " + strings.Join(missingIn, ", ") + " — a fix runs /corgi:stories and /corgi:review, which live in it",
		Fix: "in `claude` under that account: /plugin marketplace add Andriiklymiuk/corgi, then /plugin install corgi@corgi"}
}
