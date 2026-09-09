package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// claudeLaunch is the command line `corgi agent claude` runs: the binary,
// its arguments and the variables that pick the account.
type claudeLaunch struct {
	Workspace string
	Bin       string
	Args      []string
	Env       map[string]string
}

var agentClaudeCmd = &cobra.Command{
	Use:   "claude [-- claude args]",
	Short: "Run Claude Code the way this folder's workspace is configured",
	Long: `Starts Claude Code for the workspace the current directory belongs to, under
that workspace's account (configDir), binary and permission mode from the
corgi agent config — the same settings a remote session gets. Outside every
workspace it is plain claude. So one command replaces per-account aliases:
the corgi VS Code extension's "+" key runs it in a new terminal.

  corgi agent claude                 # this folder's workspace
  corgi agent claude --profile work  # under a corgi profile
  corgi agent claude --profile auto  # the listed account with most budget left
  corgi agent claude --show          # print the command instead of running it
  corgi agent claude -- --resume     # arguments after -- go to claude`,
	Run: func(cmd *cobra.Command, args []string) {
		profile, _ := cmd.Flags().GetString("profile")
		show, _ := cmd.Flags().GetBool("show")
		cwd, err := os.Getwd()
		if err != nil {
			exitWithError("agent_claude", err, 1)
		}
		launch, err := resolveClaudeLaunch(cwd, profile, args)
		if err != nil {
			exitWithError("agent_claude", err, 2)
		}
		if show {
			fmt.Println(launch.String())
			return
		}
		if launch.Workspace != "" {
			utils.Info(fmt.Sprintf("corgi: claude for %s%s", launch.Workspace, launch.accountSuffix()))
		}
		env := os.Environ()
		for k, v := range launch.Env {
			env = append(env, k+"="+v)
		}
		if err := runClaudeInPlace(launch.Bin, launch.Args, env); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				os.Exit(exit.ExitCode())
			}
			exitWithError("agent_claude", err, 1)
		}
	},
}

// execProcess is syscall.Exec, swappable so tests can watch the exec path.
var execProcess = syscall.Exec

// runClaudeInPlace replaces this process with claude where the OS allows,
// so claude's parent is the shell, not corgi. The tracking hook skips any
// claude with a corgi ancestor — that is how the daemon's own remote-control
// sessions stay off the board — and a claude run as corgi's child would be
// skipped the same way: started from the "+" key, never on a key. Where exec
// is unavailable it runs as a child and returns when claude exits.
func runClaudeInPlace(bin string, args []string, env []string) error {
	path, err := exec.LookPath(bin)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := execProcess(path, append([]string{bin}, args...), env); err == nil {
			return nil
		}
		// Exec refused (a script without a shebang, say): fall through.
	}
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = env
	return c.Run()
}

// resolveClaudeLaunch picks the workspace whose path contains dir (the
// deepest one when they nest) and turns its resolved config into a command.
func resolveClaudeLaunch(dir, profile string, extra []string) (claudeLaunch, error) {
	launch := claudeLaunch{Bin: "claude", Args: append([]string(nil), extra...), Env: map[string]string{}}
	registry, _, err := agentRegistry()
	if err != nil {
		return launch, nil
	}
	agentD, err := agentDir()
	if err != nil {
		return launch, nil
	}
	dir = cleanPath(dir)
	var id, best string
	for _, ws := range registry.Sorted() {
		root := cleanPath(ws.AbsPath)
		if root == "" || (dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator))) {
			continue
		}
		if len(root) > len(best) {
			id, best = ws.ID, root
		}
	}
	if id == "" {
		return launch, nil
	}
	launch.Workspace = id
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil {
		return launch, err
	}
	repo, _ := config.LoadRepo(best)
	resolved := config.Resolve(id, repo, user)
	if strings.EqualFold(strings.TrimSpace(profile), "auto") {
		profile = pickAccountProfile(user, resolved)
	}
	if p := strings.TrimSpace(profile); p != "" {
		withProfile, err := config.ApplyProfile(resolved, user, p)
		if err != nil {
			return launch, err
		}
		resolved = withProfile
	}
	kind, err := supervisor.KindFor(supervisor.SpawnConfig{Kind: resolved.Kind, ConfigDirEnv: resolved.ConfigDirEnv, CredentialEnv: resolved.CredentialEnv})
	if err != nil {
		return launch, err
	}
	if bin := strings.TrimSpace(resolved.Bin); bin != "" {
		launch.Bin = expandTilde(bin)
	} else if kind.DefaultBin != "" {
		launch.Bin = kind.DefaultBin
	}
	if kind.Name == supervisor.KindCustom {
		launch.Args = append(append([]string(nil), resolved.Args...), extra...)
	} else if mode := strings.TrimSpace(resolved.PermissionMode); mode != "" {
		launch.Args = append([]string{"--permission-mode", mode}, extra...)
	}
	if cfg := strings.TrimSpace(resolved.ConfigDir); cfg != "" && kind.ConfigDirEnv != "" {
		launch.Env[kind.ConfigDirEnv] = expandTilde(cfg)
	}
	return launch, nil
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(expandTilde(p)); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p)
}

func (l claudeLaunch) accountSuffix() string {
	for k, v := range l.Env {
		return " under " + v + " (" + k + ")"
	}
	return ""
}

// String is the shell form, for --show and for a terminal to run.
func (l claudeLaunch) String() string {
	parts := make([]string, 0, len(l.Env)+1+len(l.Args))
	for k, v := range l.Env {
		parts = append(parts, k+"="+shellQuote(v))
	}
	parts = append(parts, shellQuote(l.Bin))
	for _, a := range l.Args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\!*?[]{}()<>|;&") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	agentClaudeCmd.Flags().String("profile", "", "Run under this corgi profile's account and settings")
	agentClaudeCmd.Flags().Bool("show", false, "Print the resolved command and exit")
	agentCmd.AddCommand(agentClaudeCmd)
}

// pickAccountProfile is --profile auto: among the profiles the workspace's
// accounts: list allows, the one whose 5-hour window has the most room, by
// the numbers Claude Code last fetched. "" keeps the workspace's own account
// — a workspace that lists nothing never switches — and a profile with no
// snapshot yet counts as full, so an account nobody has used is tried last.
func pickAccountProfile(user *config.UserConfig, resolved config.Resolved) string {
	if user == nil || len(resolved.Accounts) == 0 {
		return ""
	}
	best, bestLeft := "", -1
	for _, name := range resolved.Accounts {
		name = strings.TrimSpace(name)
		p, ok := user.Profiles[name]
		if !ok && name != "default" {
			continue
		}
		left := 0
		if l, ok := usage.ReadLimits(expandTilde(p.ConfigDir)); ok {
			left = 100 - l.FiveHour.Percent
			if l.FiveHour.Percent >= 100 {
				left = 0
			}
		}
		if left > bestLeft {
			best, bestLeft = name, left
		}
	}
	if best == "default" {
		return ""
	}
	return best
}
