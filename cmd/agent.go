package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Keep Claude Code Remote Control running for your corgi workspaces",
	Long: `Agent mode makes this machine an always-on, multi-repo Remote Control host.

Remote Control already gives you a phone-driven Claude Code session on your own
machine. Two things stop it being always-on: the local process must keep
running, and it exits after roughly ten minutes awake without network.

corgi agent supervises it — restarting after a network timeout, holding a wake
lock so the machine does not sleep mid-session, and running one per workspace
under that workspace's own Claude config directory.

Getting started:
  corgi agent init      # in a stack, once
  corgi agent install   # start at login
  corgi agent status    # what is running, and under which account`,
}

// agentDir is where agent mode keeps daemon.json, status.json and the registry.
func agentDir() (string, error) {
	base, err := utils.CorgiDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent"), nil
}

func agentUserConfigPath(dir string) string { return filepath.Join(dir, "config.yml") }
func agentRegistryPath(dir string) string   { return filepath.Join(dir, "registry.json") }

// ---------------------------------------------------------------- serve

var agentServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Supervise Remote Control for every enabled workspace",
	Run:   runAgentServe,
}

func runAgentServe(cmd *cobra.Command, _ []string) {
	// A daemon must never stop to ask a question.
	utils.NonInteractive = true

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}

	foreground, _ := cmd.Flags().GetBool("foreground")

	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		exitWithError("agent_already_running",
			fmt.Errorf("corgi agent is already running (pid %d) — `corgi agent stop` first", info.PID), 1)
	}

	configs, err := loadSpawnConfigs(dir, foreground)
	if err != nil {
		exitWithError("agent_config", err, 1)
	}

	d := daemon.New(APP_VERSION, dir)
	printStartupDiagnostics(configs)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx, configs); err != nil && !errors.Is(err, context.Canceled) {
		exitWithError("agent_serve", err, 1)
	}
	utils.Info(art.BlueColor, "corgi agent stopped", art.WhiteColor)
}

// loadSpawnConfigs turns the registry plus both config files into launch
// settings, skipping workspaces that are unreachable or opted out.
func loadSpawnConfigs(dir string, foreground bool) ([]supervisor.SpawnConfig, error) {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return nil, err
	}

	registry.Reconcile(dirHasComposeFile)

	var out []supervisor.SpawnConfig
	for _, w := range registry.Sorted() {
		if w.Status != workspace.StatusOK {
			utils.Infof("agent: skipping %s (%s)\n", w.ID, w.Status)
			continue
		}
		repo, err := config.LoadRepo(w.AbsPath)
		if err != nil {
			utils.Infof("agent: skipping %s: %v\n", w.ID, err)
			continue
		}
		resolved := config.Resolve(w.ID, repo, user)
		if !resolved.AutostartEnabled() {
			continue
		}
		out = append(out, spawnConfigFrom(w, resolved, foreground))
	}
	return out, nil
}

func spawnConfigFrom(w workspace.Workspace, r config.Resolved, foreground bool) supervisor.SpawnConfig {
	spawn := r.Spawn
	if spawn == "" {
		// Isolate each on-demand session, so two remote sessions in one
		// workspace do not fight over a single checkout.
		spawn = "worktree"
	}
	return supervisor.SpawnConfig{
		WorkspaceID:       r.ID,
		Dir:               w.AbsPath,
		Bin:               r.Bin,
		Spawn:             spawn,
		Capacity:          r.Capacity,
		PermissionMode:    r.PermissionMode,
		ConfigDir:         r.ConfigDir,
		InheritAPIKey:     r.InheritAPIKey,
		InheritOAuthToken: r.InheritOAuthToken,
		Name:              r.ID,
		WakeLock:          supervisor.WakeLockMode(r.WakeLock),
		MirrorOutput:      foreground,
	}
}

func dirHasComposeFile(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"corgi-compose.yml", "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// printStartupDiagnostics is the one line per workspace that prevents the most
// likely surprise: work running under the wrong Claude account.
func printStartupDiagnostics(configs []supervisor.SpawnConfig) {
	env := os.Environ()
	for _, c := range configs {
		configDir := c.ConfigDir
		if configDir == "" {
			configDir = "<default>"
		}
		utils.Infof("agent: %-20s dir=%s configDir=%s spawn=%s\n", c.WorkspaceID, c.Dir, configDir, c.Spawn)
		if stripped := supervisor.StrippedCredentials(c, env); len(stripped) > 0 {
			utils.Infof("agent: %-20s stripped from child env: %v\n", c.WorkspaceID, stripped)
		}
		if c.InheritAPIKey {
			utils.Infof("agent: %-20s WARNING inheriting ANTHROPIC_API_KEY — remote control refuses to start with one set\n", c.WorkspaceID)
		}
	}
}

// ---------------------------------------------------------------- status

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what agent mode is running, and under which account",
	Run:   runAgentStatus,
}

func runAgentStatus(_ *cobra.Command, _ []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	status, err := daemon.ReadStatus(dir)
	if err != nil {
		exitWithError("agent_status", err, 1)
	}
	if status == nil {
		status = &daemon.Status{Running: false, WakeLockable: supervisor.Supported()}
	}

	if utils.JSONOutput {
		utils.PrintJSON(status)
		return
	}

	if !status.Running {
		fmt.Println("corgi agent is not running. Start it with `corgi agent serve`, or `corgi agent install` to start at login.")
		return
	}

	fmt.Printf("corgi agent running (pid %d, version %s)\n", status.PID, status.Version)
	if !status.WakeLockable {
		fmt.Println("wake lock: unsupported on this platform")
	}
	for _, w := range status.Workspaces {
		state := "stopped"
		if w.Running {
			state = "running"
		}
		if w.Disabled {
			state = "disabled"
		}
		fmt.Printf("  %-20s %-9s restarts=%d wakeLock=%v\n", w.WorkspaceID, state, w.Restarts, w.WakeLock)
		if w.LastReason != "" {
			fmt.Printf("  %-20s %s\n", "", w.LastReason)
		}
	}
	for _, d := range status.Diagnostics {
		fmt.Printf("  %-20s bin=%s configDir=%s\n", d.WorkspaceID, d.Bin, d.ConfigDir)
		if len(d.Stripped) > 0 {
			fmt.Printf("  %-20s stripped: %v\n", "", d.Stripped)
		}
		if d.Warning != "" {
			fmt.Printf("  %-20s %s %s%s\n", "", art.RedColor, d.Warning, art.WhiteColor)
		}
	}
}

// ---------------------------------------------------------------- stop

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the agent daemon",
	Run:   runAgentStop,
}

func runAgentStop(_ *cobra.Command, _ []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		exitWithError("agent_stop", err, 1)
	}
	if info == nil {
		utils.Info("corgi agent is not running")
		return
	}
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		exitWithError("agent_stop", err, 1)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		exitWithError("agent_stop", fmt.Errorf("could not stop pid %d: %w", info.PID, err), 1)
	}
	utils.Infof("stopping corgi agent (pid %d)\n", info.PID)
}

// ---------------------------------------------------------------- workspaces

var agentWorkspacesCmd = &cobra.Command{
	Use:     "workspaces",
	Aliases: []string{"ws"},
	Short:   "List and manage the workspaces agent mode knows about",
	Run:     runAgentWorkspacesList,
}

var agentWorkspacesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered workspaces",
	Run:   runAgentWorkspacesList,
}

func runAgentWorkspacesList(_ *cobra.Command, _ []string) {
	registry, path := mustLoadRegistry()
	registry.Reconcile(dirHasComposeFile)
	_ = workspace.Save(path, registry)

	if utils.JSONOutput {
		utils.PrintJSON(registry.Sorted())
		return
	}
	if len(registry.Workspaces) == 0 {
		fmt.Println("No workspaces registered. Run `corgi agent init` in a stack, or `corgi agent scan <dir>`.")
		return
	}
	for _, w := range registry.Sorted() {
		fmt.Printf("%-20s %-12s %s\n", w.ID, w.Status, w.AbsPath)
		if len(w.Aliases) > 0 {
			fmt.Printf("%-20s also known as: %v\n", "", w.Aliases)
		}
	}
}

var agentWorkspacesForgetCmd = &cobra.Command{
	Use:   "forget <id>",
	Short: "Remove a workspace from the registry",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		registry, path := mustLoadRegistry()
		if !registry.Forget(args[0]) {
			exitWithError("agent_workspace_unknown", fmt.Errorf("no workspace called %q", args[0]), 1)
		}
		if err := workspace.Save(path, registry); err != nil {
			exitWithError("agent_registry_write", err, 1)
		}
		utils.Infof("forgot %s\n", args[0])
	},
}

var agentWorkspacesRelocateCmd = &cobra.Command{
	Use:   "relocate <id> <path>",
	Short: "Point a workspace at a new directory",
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		registry, path := mustLoadRegistry()
		existing, ok := registry.Find(args[0])
		if !ok {
			exitWithError("agent_workspace_unknown", fmt.Errorf("no workspace called %q", args[0]), 1)
		}
		abs, err := filepath.Abs(args[1])
		if err != nil {
			exitWithError("agent_bad_path", err, 2)
		}
		existing.AbsPath = abs
		existing.Status = workspace.StatusOK
		registry.Upsert(existing)
		if err := workspace.Save(path, registry); err != nil {
			exitWithError("agent_registry_write", err, 1)
		}
		utils.Infof("%s now points at %s\n", existing.ID, abs)
	},
}

var agentResolveCmd = &cobra.Command{
	Use:   "resolve <name>",
	Short: "Show which workspace a name resolves to, without starting anything",
	Args:  cobra.MinimumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		registry, _ := mustLoadRegistry()
		query := args[0]
		for _, a := range args[1:] {
			query += " " + a
		}
		res := workspace.Resolve(registry, query)

		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		if res.Resolved() {
			fmt.Println(res.Reason)
			return
		}
		fmt.Println(res.Reason)
		for _, c := range res.Candidates {
			fmt.Printf("  %-20s %s\n", c.Workspace.ID, c.Workspace.AbsPath)
		}
		// Ambiguity is a question for a person, not a failure of the machine,
		// but it must not read as success to a script.
		os.Exit(2)
	},
}

// mustLoadRegistry is the CLI's view of the same registry the MCP tools read.
func mustLoadRegistry() (*workspace.Registry, string) {
	registry, path, err := agentRegistry()
	if err != nil {
		exitWithError("agent_registry_read", err, 1)
	}
	return registry, path
}

func init() {
	agentServeCmd.Flags().Bool("foreground", false,
		"Run in this terminal and mirror the supervised process's output. Off by default: that output can contain env values and tokens, and in serve mode corgi's stderr is a log file.")

	agentWorkspacesCmd.AddCommand(agentWorkspacesListCmd, agentWorkspacesForgetCmd, agentWorkspacesRelocateCmd)
	agentCmd.AddCommand(
		agentServeCmd,
		agentStatusCmd,
		agentStopCmd,
		agentWorkspacesCmd,
		agentResolveCmd,
	)
	rootCmd.AddCommand(agentCmd)
}
