package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if !dirHasComposeFile(cwd) {
		exitWithError("agent_no_compose",
			fmt.Errorf("no corgi-compose.yml here — run this in a stack, or `corgi agent scan <dir>` to find them"), 2)
	}

	id, _ := cmd.Flags().GetString("id")
	if id == "" {
		id = filepath.Base(cwd)
	}
	aliases, _ := cmd.Flags().GetStringSlice("alias")
	configDir, _ := cmd.Flags().GetString("config-dir")
	sensitive, _ := cmd.Flags().GetBool("sensitive")

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
	registry.Upsert(existing)
	if err := workspace.Save(path, registry); err != nil {
		exitWithError("agent_registry_write", err, 1)
	}

	if configDir != "" {
		if err := setUserWorkspaceConfigDir(id, configDir); err != nil {
			exitWithError("agent_write_user_config", err, 1)
		}
	}

	utils.Infof("registered %s (%s)\n", id, cwd)
	utils.Info("wrote .corgi/agent.yml — safe to commit, it holds identity only")
	if configDir == "" {
		utils.Info("no config dir set: this workspace uses your default Claude account.")
		utils.Info("If you keep work and personal logins separate, set one:")
		utils.Infof("  corgi agent init --config-dir ~/.claude-work\n")
	}
	utils.Info("next: `corgi agent install` to start at login, then `corgi agent status`")
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

// setUserWorkspaceConfigDir records the Claude config directory for a workspace
// in the trusted user-level file, creating it with owner-only permissions.
func setUserWorkspaceConfigDir(id, configDir string) error {
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
	entry.ConfigDir = configDir
	user.Workspaces[id] = entry

	body, err := yaml.Marshal(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// 0600: this file names the directories holding Claude credentials.
	return os.WriteFile(path, body, 0o600)
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
		registry.Upsert(workspace.Workspace{
			ID:          id,
			AbsPath:     dir,
			ComposeFile: composeFileName(dir),
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
			Name:   "wake lock",
			Detail: "unsupported on " + runtime.GOOS,
			Fix:    "sessions will end if the machine sleeps",
		}
	}
	argv := supervisor.WakeLockCommand(os.Getpid())
	if _, err := exec.LookPath(argv[0]); err != nil {
		return agentCheck{
			Name:   "wake lock",
			Detail: argv[0] + " not found",
			Fix:    "install " + argv[0] + ", or set wakeLock: off",
		}
	}
	detail := argv[0]
	if runtime.GOOS == "darwin" {
		detail += " — note: " + supervisor.ClamshellWarning
	}
	return agentCheck{Name: "wake lock", OK: true, Detail: detail}
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
		return agentCheck{Name: "user config", OK: true, Detail: "none yet (defaults apply)"}
	}
	if err != nil {
		return agentCheck{Name: "user config", Detail: err.Error()}
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return agentCheck{
			Name:   "user config",
			Detail: fmt.Sprintf("%s is readable by other users (mode %04o)", path, mode),
			Fix:    "chmod 600 " + path,
		}
	}
	return agentCheck{Name: "user config", OK: true, Detail: path}
}

func checkRegisteredWorkspaces() agentCheck {
	registry, _ := mustLoadRegistry()
	registry.Reconcile(dirHasComposeFile)

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
	agentInitCmd.Flags().StringSlice("alias", nil, "Extra names this workspace answers to, e.g. --alias 'todo app'")
	agentInitCmd.Flags().String("config-dir", "", "CLAUDE_CONFIG_DIR for this workspace, so it runs under a specific Claude account")
	agentInitCmd.Flags().Bool("sensitive", false, "Never open a public tunnel for this workspace")

	agentScanCmd.Flags().Bool("dry-run", false, "Show what would be registered without changing anything")

	agentCmd.AddCommand(agentInitCmd, agentScanCmd, agentDoctorCmd)
}
