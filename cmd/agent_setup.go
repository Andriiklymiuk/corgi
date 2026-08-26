package cmd

import (
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
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------- init

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
	if !dirIsWorkspace(cwd) {
		exitWithError("agent_no_workspace",
			fmt.Errorf("nothing to register here — run this in a corgi stack or a git repository (or `corgi agent scan <dir>` to find stacks)"), 2)
	}

	id, _ := cmd.Flags().GetString("id")
	if id == "" {
		id = filepath.Base(cwd)
	}
	aliases, _ := cmd.Flags().GetStringSlice("alias")
	configDir, _ := cmd.Flags().GetString("config-dir")
	sensitive, _ := cmd.Flags().GetBool("sensitive")
	skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")

	if err := writeRepoAgentConfig(cwd, id, aliases, sensitive); err != nil {
		exitWithError("agent_write_repo_config", err, 1)
	}

	registry, path := mustLoadRegistry()
	existing, _ := registry.Find(id)
	existing.ID = id
	existing.AbsPath = cwd
	existing.ComposeFile = composeFileName(cwd)
	existing.Aliases = aliases
	existing.Status = workspace.StatusOK
	// Cache the service names so "fix the api" can resolve to the stack that
	// has a service called api. Without this the resolver's service matching
	// has nothing to match against.
	existing.Services, existing.Repos = describeStack(cwd)
	registry.Upsert(existing)
	if err := workspace.Save(path, registry); err != nil {
		exitWithError("agent_registry_write", err, 1)
	}

	// init is the deliberate opt-in, so it is what turns supervision on.
	// `corgi agent scan` registers without arming anything.
	if err := enableWorkspace(id, configDir, skipPerms); err != nil {
		exitWithError("agent_write_user_config", err, 1)
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
	utils.Info("next: `corgi agent install` to start at login, then `corgi agent status`")
}

// describeStack reads a stack's service and repository names for the registry,
// so a phone can say "fix the api" and reach the right workspace.
//
// The compose file is parsed directly rather than through GetCorgiServices:
// that path mutates global cobra flags, resolves environments, and can prompt
// when a directory turns out not to hold a stack — none of which belongs in a
// best-effort read that runs over whatever `agent scan` walked past.
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

// enableWorkspace turns on supervision for a workspace, and records its Claude
// config directory, in the trusted user-level file.
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
	// Never silently clears an already-set opt-in: a re-init without the flag
	// leaves a previously-granted bypass alone, matching how the config overlay
	// OR-s capability booleans rather than overwriting them.
	if skipPerms {
		entry.DangerouslySkipPermissions = true
	}
	user.Workspaces[id] = entry

	return writeUserConfig(path, user)
}

// ---------------------------------------------------------------- scan

var agentScanCmd = &cobra.Command{
	Use:   "scan <dir>",
	Short: "Find corgi stacks under a directory and register them",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentScan,
}

// scanMaxDepth bounds the walk. Stacks live a couple of levels under a projects
// directory; descending further mostly finds node_modules.
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

// skipDirs are never worth descending into when hunting for stacks.
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
			return nil // an unreadable directory is not worth failing the scan
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
			return filepath.SkipDir // a stack does not contain another stack
		}
		return nil
	})
	return out
}

// ---------------------------------------------------------------- doctor

var agentDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check whether agent mode can actually work here",
	Run:   runAgentDoctor,
}

// Check names, kept as constants so the same string is not repeated across a
// check and its assertions.
const (
	checkWakeLock   = "wake lock"
	checkUserConfig = "user config"
)

type agentCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func runAgentDoctor(_ *cobra.Command, _ []string) {
	checks := collectAgentChecks()

	if utils.JSONOutput {
		utils.PrintJSON(checks)
		if anyCheckFailed(checks) {
			os.Exit(1)
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
		os.Exit(1)
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

	checks = append(checks, checkClaudeBinary(), checkAmbientAPIKey(), checkWakeLockSupport(), checkInstallSupport())

	dir, err := agentDir()
	if err != nil {
		return append(checks, agentCheck{Name: "data directory", Detail: err.Error()})
	}
	checks = append(checks, checkUserConfigPermissions(agentUserConfigPath(dir)), checkRegisteredWorkspaces(), checkDaemonRunning(dir))
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
	detail := argv[0]
	if runtime.GOOS == "darwin" {
		detail += " — note: " + supervisor.ClamshellWarning
	}
	return agentCheck{Name: checkWakeLock, OK: true, Detail: detail}
}

func checkInstallSupport() agentCheck {
	if !installSupported() {
		return agentCheck{
			Name:   "start at login",
			Detail: "not supported on " + runtime.GOOS,
			Fix:    "run `corgi agent serve` yourself, or supervise it with your own tooling",
		}
	}
	return agentCheck{Name: "start at login", OK: true, Detail: installMechanism()}
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

	agentCmd.AddCommand(agentInitCmd, agentScanCmd, agentDoctorCmd)
}
