package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/supervisor"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// A profile is a named account bundle in the trusted user config, picked at
// session-start time with --profile. It lives in the GLOBAL corgi data dir, not
// a repo's .corgi/agent.yml: choosing a config directory or binary grants
// capability, and a committed file must never do that (a clone would run under
// your login). See utils/agent/config for the trust split.

var agentProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage named Claude account profiles for remote session start",
	Long: `Profiles are named account bundles you pick at start time:

  corgi agent profile add work --config-dir ~/.claude-work
  corgi agent session start my-stack --profile work

They live in the global corgi config (never a committed repo file), because a
profile chooses which config directory and binary run — that is capability, and
capability must not travel with a clone.`,
	Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() },
}

var agentProfileAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add or update a profile",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		configDir, _ := cmd.Flags().GetString("config-dir")
		bin, _ := cmd.Flags().GetString("bin")
		permissionMode, _ := cmd.Flags().GetString("permission-mode")
		skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")
		dir := mustAgentDir()
		if err := addProfile(dir, args[0], config.WorkspaceConfig{
			ConfigDir: configDir, Bin: bin, PermissionMode: permissionMode,
			DangerouslySkipPermissions: skipPerms,
		}); err != nil {
			exitWithError("agent_profile_add", err, 1)
		}
		utils.Infof("profile %q saved — use it with `corgi agent session start <workspace> --profile %s`\n", args[0], args[0])
	},
}

var agentProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List defined profiles",
	Run: func(_ *cobra.Command, _ []string) {
		profiles, err := loadProfiles(mustAgentDir())
		if err != nil {
			exitWithError("agent_profile_list", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(profiles)
			return
		}
		if len(profiles) == 0 {
			fmt.Println("No profiles defined. Add one with `corgi agent profile add <name> --config-dir <dir>`.")
			return
		}
		for _, name := range sortedProfileNames(profiles) {
			p := profiles[name]
			cfgDir := p.ConfigDir
			if cfgDir == "" {
				cfgDir = "<default account>"
			}
			fmt.Printf("%-16s configDir=%s", name, cfgDir)
			if p.Bin != "" {
				fmt.Printf(" bin=%s", p.Bin)
			}
			if p.PermissionMode != "" {
				fmt.Printf(" permissionMode=%s", p.PermissionMode)
			}
			if p.DangerouslySkipPermissions {
				fmt.Print(" ⚠ permissions=SKIPPED")
			}
			fmt.Println()
		}
	},
}

var agentProfileRemoveCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove"},
	Short:   "Remove a profile",
	Args:    cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		removed, err := removeProfile(mustAgentDir(), args[0])
		if err != nil {
			exitWithError("agent_profile_rm", err, 1)
		}
		if !removed {
			exitWithError("agent_profile_unknown", fmt.Errorf("no profile called %q", args[0]), 1)
		}
		utils.Infof("removed profile %q\n", args[0])
	},
}

// addProfile writes one profile into the trusted user config, creating the file
// and section as needed. It rejects a profile that grants nothing and a binary
// that is a path (which would let this choose an arbitrary program to run).
func addProfile(dir, name string, wc config.WorkspaceConfig) error {
	if name == "" {
		return fmt.Errorf("a profile name is required")
	}
	if wc.ConfigDir == "" && wc.Bin == "" && !wc.DangerouslySkipPermissions {
		return fmt.Errorf("a profile must set at least --config-dir, --bin, or --dangerously-skip-permissions, or it does nothing")
	}
	if _, err := supervisor.SanitizeBin(wc.Bin); err != nil {
		return err
	}
	if !supervisor.ValidPermissionMode(wc.PermissionMode) {
		return fmt.Errorf("unknown or disallowed permissionMode %q (want one of: %s)",
			wc.PermissionMode, supervisor.PermissionModeHint())
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	if user.Profiles == nil {
		user.Profiles = map[string]config.WorkspaceConfig{}
	}
	user.Profiles[name] = wc
	return writeUserConfig(path, user)
}

// removeProfile deletes a profile, reporting whether it existed.
func removeProfile(dir, name string) (bool, error) {
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return false, err
	}
	if _, ok := user.Profiles[name]; !ok {
		return false, nil
	}
	delete(user.Profiles, name)
	return true, writeUserConfig(path, user)
}

func loadProfiles(dir string) (map[string]config.WorkspaceConfig, error) {
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return nil, err
	}
	return user.Profiles, nil
}

func sortedProfileNames(profiles map[string]config.WorkspaceConfig) []string {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// writeUserConfig persists the trusted user config at 0600. The file names the
// directories holding Claude credentials, so it is never group- or
// world-readable.
func writeUserConfig(path string, user *config.UserConfig) error {
	body, err := yaml.Marshal(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func mustAgentDir() string {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	return dir
}

func init() {
	agentProfileAddCmd.Flags().String("config-dir", "", "Claude config directory for this profile (e.g. ~/.claude-work) — the account it runs under")
	agentProfileAddCmd.Flags().String("bin", "", "Command to run instead of the default `claude` (a real program on PATH, not a shell alias)")
	agentProfileAddCmd.Flags().String("permission-mode", "", "Permission mode passed to remote control (default|acceptEdits|plan|auto|dontask)")
	agentProfileAddCmd.Flags().Bool("dangerously-skip-permissions", false,
		"Run this profile's sessions with permission prompts OFF (--permission-mode bypassPermissions). Removes the gate you answer from your phone — trusted local config only, off by default.")
	agentProfileCmd.AddCommand(agentProfileAddCmd, agentProfileListCmd, agentProfileRemoveCmd)
	agentCmd.AddCommand(agentProfileCmd)
}
