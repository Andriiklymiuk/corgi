package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	// Cobra runs only the nearest PersistentPreRun, so replicate the root's
	// global-flag handling, then warn once if agent data was left at the old
	// location by the data-dir move.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		applyGlobalFlags(cmd)
		warnStrandedAgentData()
	},
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
//
// It uses the per-user data directory (NativeDataDir), never the Homebrew
// prefix: a registry of paths and per-device tokens is user data, and a brew
// reinstall must not wipe it. Agent mode is new, so there is no legacy data to
// carry across.
func agentDir() (string, error) {
	base, err := utils.NativeDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent"), nil
}

var legacyAgentWarnOnce sync.Once

// warnStrandedAgentData says once, if agent data exists at the old Homebrew-var
// location but not the new per-user one, that the location changed and the old
// setup is not carried over. Called from command entry points (never agentDir,
// so path resolution stays side-effect-free); a plain notice, never a move —
// agent mode is unreleased, so there is nothing to migrate for real users.
func warnStrandedAgentData() {
	legacyAgentWarnOnce.Do(func() {
		newDir, err := agentDir()
		if err != nil {
			return
		}
		if _, err := os.Stat(newDir); err == nil {
			return // already using the new location
		}
		legacyBase, err := utils.CorgiDataDir()
		if err != nil {
			return
		}
		legacy := filepath.Join(legacyBase, "agent")
		if legacy == newDir {
			return // no separate legacy location (CORGI_DATA_DIR override, etc.)
		}
		if info, statErr := os.Stat(legacy); statErr != nil || !info.IsDir() {
			return // nothing stranded
		}
		utils.Infof("corgi: agent data now lives at %s (was %s).\n"+
			"The old setup is not carried over — re-run `corgi agent init` and re-pair your devices.\n",
			newDir, legacy)
	})
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
	d.CaptureBrief = captureWorkspaceBrief
	d.ResolveWorkspace = remoteResolver(dir, foreground)
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
		if cfg, ok := spawnConfigForWorkspace(w, user, foreground); ok {
			cfg.Origin = supervisor.OriginAutostart
			out = append(out, cfg)
		}
	}
	return out, nil
}

// remoteResolver builds launch settings for one workspace on demand,
// reloading registry and config so a remote start sees the current files,
// not the ones from daemon startup. No autostart check: a remote start IS
// the explicit act autostart substitutes for.
func remoteResolver(dir string, foreground bool) func(id, profile string) (supervisor.SpawnConfig, error) {
	return func(id, profile string) (supervisor.SpawnConfig, error) {
		registry, err := workspace.Load(agentRegistryPath(dir))
		if err != nil {
			return supervisor.SpawnConfig{}, err
		}
		registry.Reconcile(dirHasComposeFile)
		w, ok := registry.Find(id)
		if !ok {
			return supervisor.SpawnConfig{}, fmt.Errorf("no workspace called %q in the registry — run `corgi agent scan` on the laptop", id)
		}
		if w.Status != workspace.StatusOK {
			return supervisor.SpawnConfig{}, fmt.Errorf("workspace %s is %s — fix the path with `corgi agent workspaces relocate`", w.ID, w.Status)
		}
		user, err := config.LoadUser(agentUserConfigPath(dir))
		if err != nil {
			return supervisor.SpawnConfig{}, err
		}
		repo, err := config.LoadRepo(w.AbsPath)
		if err != nil {
			return supervisor.SpawnConfig{}, err
		}
		resolved := config.Resolve(w.ID, repo, user)
		if resolved.Sensitive {
			// A workspace the repo marked sensitive has opted out of being
			// driven remotely. Same refusal the preview tunnels give it, so the
			// flag means one thing everywhere.
			return supervisor.SpawnConfig{}, fmt.Errorf("workspace %s is marked sensitive — remote session start is refused (start it on the laptop, or unset sensitive in .corgi/agent.yml)", w.ID)
		}
		resolved, err = config.ApplyProfile(resolved, user, profile)
		if err != nil {
			return supervisor.SpawnConfig{}, err
		}
		cfg := spawnConfigFrom(w, resolved, foreground)
		cfg.Origin = supervisor.OriginRemote
		cfg.Profile = profile
		return cfg, nil
	}
}

// spawnConfigForWorkspace decides whether one registered workspace should be
// supervised, and with what settings. Anything skipped says why, since a
// workspace silently not starting is the confusing failure here.
func spawnConfigForWorkspace(w workspace.Workspace, user *config.UserConfig, foreground bool) (supervisor.SpawnConfig, bool) {
	if w.Status != workspace.StatusOK {
		utils.Infof("agent: skipping %s (%s)\n", w.ID, w.Status)
		return supervisor.SpawnConfig{}, false
	}
	repo, err := config.LoadRepo(w.AbsPath)
	if err != nil {
		utils.Infof("agent: skipping %s: %v\n", w.ID, err)
		return supervisor.SpawnConfig{}, false
	}
	resolved := config.Resolve(w.ID, repo, user)
	if !resolved.AutostartEnabled() {
		// Every other skip explains itself; this one is the most likely to be
		// unexpected, since `corgi agent scan` registers without enabling.
		utils.Infof("agent: skipping %s (not enabled — run `corgi agent init` there, or set autostart: true)\n", w.ID)
		return supervisor.SpawnConfig{}, false
	}
	return spawnConfigFrom(w, resolved, foreground), true
}

func spawnConfigFrom(w workspace.Workspace, r config.Resolved, foreground bool) supervisor.SpawnConfig {
	cfg := supervisor.SpawnConfig{
		WorkspaceID:       r.ID,
		Dir:               w.AbsPath,
		Kind:              r.Kind,
		Bin:               r.Bin,
		Args:              r.Args,
		ConfigDirEnv:      r.ConfigDirEnv,
		CredentialEnv:     r.CredentialEnv,
		Spawn:             r.Spawn,
		Capacity:          r.Capacity,
		PermissionMode:    r.PermissionMode,
		ConfigDir:         r.ConfigDir,
		InheritAPIKey:     r.InheritAPIKey,
		InheritOAuthToken: r.InheritOAuthToken,
		WakeLock:          supervisor.WakeLockMode(r.WakeLock),
		MirrorOutput:      foreground,
	}
	kind, err := supervisor.KindFor(cfg)
	if err != nil {
		// An unknown kind is reported by ValidateSpawnConfig with the valid
		// names; filling in defaults for it here would only mask that.
		return cfg
	}
	if kind.BuildsArgvFromSettings {
		// The session name shown in claude.ai/code. Meaningless to a kind handed
		// a complete argv, where it would be a setting that never takes effect.
		cfg.Name = r.ID
	}
	if cfg.Spawn == "" && kind.SupportsSpawn {
		// Isolate each on-demand session, so two remote sessions in one
		// workspace do not fight over a single checkout.
		cfg.Spawn = "worktree"
	}
	return cfg
}

// dirHasComposeFile reports whether dir is a corgi stack.
//
// It requires an actual compose file. An earlier version fell back to "is a
// directory", which made the guard in `corgi agent init` dead: running it in
// any folder registered that folder and the daemon then supervised it.
func dirHasComposeFile(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"corgi-compose.yml", "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
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
		kind := c.Kind
		if kind == "" {
			kind = supervisor.DefaultKind
		}
		utils.Infof("agent: %-20s dir=%s kind=%s configDir=%s spawn=%s\n", c.WorkspaceID, c.Dir, kind, configDir, c.Spawn)
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
		printWorkspaceState(w)
	}
	for _, d := range status.Diagnostics {
		printWorkspaceDiagnostic(d)
	}
}

func printWorkspaceState(w supervisor.RunState) {
	fmt.Printf("  %-20s %-9s restarts=%d wakeLock=%v\n", w.WorkspaceID, workspaceState(w), w.Restarts, w.WakeLock)
	if w.LastReason != "" {
		fmt.Printf("  %-20s %s\n", "", w.LastReason)
	}
	if w.Origin == supervisor.OriginRemote {
		label := "started remotely"
		if w.Profile != "" {
			label += " · profile " + w.Profile
		}
		fmt.Printf("  %-20s %s\n", "", label)
	}
	if w.SessionURL != "" {
		fmt.Printf("  %-20s %s\n", "", w.SessionURL)
	}
}

// workspaceState collapses the flags into the one word worth reading first.
// Disabled outranks running: a disabled workspace is the thing to explain.
func workspaceState(w supervisor.RunState) string {
	switch {
	case w.Disabled:
		return "disabled"
	case w.Running:
		return "running"
	default:
		return "stopped"
	}
}

func printWorkspaceDiagnostic(d daemon.WorkspaceDiagnostic) {
	fmt.Printf("  %-20s bin=%s configDir=%s\n", d.WorkspaceID, d.Bin, d.ConfigDir)
	if len(d.Stripped) > 0 {
		fmt.Printf("  %-20s stripped: %v\n", "", d.Stripped)
	}
	if d.Warning != "" {
		fmt.Printf("  %-20s %s %s%s\n", "", art.RedColor, d.Warning, art.WhiteColor)
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
		// Drop the handover note too. Reusing an id for a different stack later
		// would otherwise surface branches belonging to something else.
		if dir, dirErr := agentDir(); dirErr == nil {
			_ = brief.Clear(dir, args[0])
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
		// Same reason `forget` drops it: the brief holds the old stack's repo
		// paths and branches, and keeping it would have `corgi agent brief`
		// describe a directory this id no longer points at.
		if dir, dirErr := agentDir(); dirErr == nil {
			_ = brief.Clear(dir, existing.ID)
		}
		utils.Infof("%s now points at %s\n", existing.ID, abs)
	},
}

// ---------------------------------------------------------------- session

var agentSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Start or stop a supervised session in a workspace, on demand",
}

var agentSessionStartCmd = &cobra.Command{
	Use:   "start <workspace>",
	Short: "Ask the running daemon to start a session in a workspace",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		profile, _ := cmd.Flags().GetString("profile")
		enqueueSessionCommand(command.ActionStart, args, profile)
	},
}

var agentSessionStopCmd = &cobra.Command{
	Use:   "stop <workspace>",
	Short: "Ask the running daemon to stop a workspace's session",
	Args:  cobra.MinimumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		enqueueSessionCommand(command.ActionStop, args, "")
	},
}

// enqueueSessionCommand resolves the workspace, writes the spool command and
// nudges the daemon — the same path the MCP tools use, so the two surfaces
// cannot drift.
func enqueueSessionCommand(action string, args []string, profile string) {
	registry, _ := mustLoadRegistry()
	registry.Reconcile(dirHasComposeFile)
	res := workspace.Resolve(registry, strings.Join(args, " "))
	if !res.Resolved() {
		if utils.JSONOutput {
			utils.PrintJSON(res)
		} else {
			fmt.Println(res.Reason)
			for _, c := range res.Candidates {
				fmt.Printf("  %-20s %s\n", c.Workspace.ID, c.Workspace.AbsPath)
			}
		}
		os.Exit(2)
	}

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		exitWithError("agent_session", err, 1)
	}
	if info == nil {
		exitWithError("agent_not_running",
			errors.New("corgi agent is not running — start it with `corgi agent serve`, or `corgi agent install` to start at login"), 1)
	}
	if !info.Commands {
		exitWithError("agent_no_command_support",
			errors.New("the running corgi agent predates remote session start — restart it: `corgi agent stop` then `corgi agent serve`"), 1)
	}

	c, err := command.Write(dir, command.Command{
		Action: action, WorkspaceID: res.Workspace.ID, Profile: profile, Source: "cli",
	})
	if err != nil {
		exitWithError("agent_session", err, 1)
	}
	daemon.Nudge(info)

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"queued": action, "workspaceId": res.Workspace.ID, "commandId": c.ID})
		return
	}
	utils.Infof("%s queued for %s — watch `corgi agent status` for the session URL\n", action, res.Workspace.ID)
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
			// Same exit code as the human path: a script must not read an
			// ambiguous answer as a resolved one.
			if !res.Resolved() {
				os.Exit(2)
			}
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

	agentBriefCmd.Flags().Bool("json", false, "Machine-readable output")

	agentSessionStartCmd.Flags().String("profile", "", "Profile from the agent config's profiles: section (e.g. work)")
	agentSessionCmd.AddCommand(agentSessionStartCmd, agentSessionStopCmd)

	agentWorkspacesCmd.AddCommand(agentWorkspacesListCmd, agentWorkspacesForgetCmd, agentWorkspacesRelocateCmd)
	agentCmd.AddCommand(
		agentServeCmd,
		agentStatusCmd,
		agentStopCmd,
		agentSessionCmd,
		agentWorkspacesCmd,
		agentResolveCmd,
		agentBriefCmd,
	)
	rootCmd.AddCommand(agentCmd)
}
