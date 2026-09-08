package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

var agentCarryCmd = &cobra.Command{
	Use:   "carry <session> --profile <name>",
	Short: "Continue a session under another account, conversation and all",
	Long: `Moves a session to another of the workspace's accounts: its transcript is
copied into that account's config directory and a new terminal in the same
window runs ` + "`corgi agent claude --profile <name> -- --resume <id>`" + `, so
the conversation carries on where it was. For the session that hit its limit
while the other account still has budget.

Only a profile the workspace's accounts: list names is allowed — a workspace
that lists none cannot be carried anywhere. The old session is dismissed once
the new one is up; its process is left alone.`,
	Args: cobra.ExactArgs(1),
	Run:  runAgentCarry,
}

func runAgentCarry(cmd *cobra.Command, args []string) {
	profile, _ := cmd.Flags().GetString("profile")
	profile = strings.TrimSpace(profile)
	if profile == "" {
		exitWithError("agent_carry", fmt.Errorf("--profile is required: which account to carry to"), 2)
	}
	dir := mustAgentDir()
	board, err := readBoard(dir)
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	s, err := findBoardSession(board.State, args[0])
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	plan, err := planCarry(dir, s, profile)
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	if err := os.MkdirAll(filepath.Dir(plan.To), 0o700); err != nil {
		exitWithError("agent_carry", err, 1)
	}
	if err := copyFile(plan.From, plan.To); err != nil {
		exitWithError("agent_carry", fmt.Errorf("copy transcript: %w", err), 1)
	}
	sendBoardCommand(command.Command{Action: command.ActionNew, WindowID: s.Host.WindowID, Command: plan.Command, Source: "cli"},
		fmt.Sprintf("carrying %s to %s: a new terminal resumes it there", s.Display, profile))
	if s.Status == sessions.StatusLimited || s.Status == sessions.StatusDone || s.Status == sessions.StatusStale {
		_, _ = command.Write(dir, command.Command{Action: command.ActionDismiss, SessionID: s.ID, Source: "cli"})
	}
	if !utils.JSONOutput {
		utils.Info("the old session keeps running; close its tab when you are done with it")
	}
}

type carryPlan struct {
	From, To string
	Command  string
}

// planCarry checks the move is allowed and works out the paths. It reads
// the workspace's accounts list from the trusted user config: a committed
// file can never widen where a conversation may go.
func planCarry(agentD string, s sessions.Session, profile string) (carryPlan, error) {
	if s.Cwd == "" {
		return carryPlan{}, fmt.Errorf("%s has no working directory on record", s.Display)
	}
	if sessions.Placeholder(s.ID) {
		return carryPlan{}, fmt.Errorf("%s has not reported its session id yet", s.Display)
	}
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil {
		return carryPlan{}, err
	}
	launch, err := resolveClaudeLaunch(s.Cwd, "", nil)
	if err != nil {
		return carryPlan{}, err
	}
	if launch.Workspace == "" {
		return carryPlan{}, fmt.Errorf("%s is not inside a registered workspace", s.Cwd)
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return carryPlan{}, err
	}
	ws, ok := registry.Find(launch.Workspace)
	if !ok {
		return carryPlan{}, fmt.Errorf("workspace %s is not registered", launch.Workspace)
	}
	repo, _ := config.LoadRepo(ws.AbsPath)
	resolved := config.Resolve(launch.Workspace, repo, user)
	if !accountAllowed(resolved.Accounts, profile) {
		if len(resolved.Accounts) == 0 {
			return carryPlan{}, fmt.Errorf("workspace %s lists no accounts: add `accounts: [default, %s]` under it in %s", launch.Workspace, profile, agentUserConfigPath(agentD))
		}
		return carryPlan{}, fmt.Errorf("workspace %s may run under %s, not %s", launch.Workspace, strings.Join(resolved.Accounts, ", "), profile)
	}
	target, err := config.ApplyProfile(resolved, user, profile)
	if err != nil {
		return carryPlan{}, err
	}
	fromDir := claudeConfigDir(s.ConfigDir)
	toDir := claudeConfigDir(expandTilde(target.ConfigDir))
	if samePath(fromDir, toDir) {
		return carryPlan{}, fmt.Errorf("%s already runs under %s", s.Display, profile)
	}
	rel := filepath.Join("projects", mungeClaudeProjectDir(s.Cwd), s.ID+".jsonl")
	plan := carryPlan{From: filepath.Join(fromDir, rel), To: filepath.Join(toDir, rel)}
	if _, err := os.Stat(plan.From); err != nil {
		return carryPlan{}, fmt.Errorf("no transcript for %s at %s", s.Display, plan.From)
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "corgi"
	}
	plan.Command = fmt.Sprintf("%s agent claude --profile %s -- --resume %s", shellQuote(exe), shellQuote(profile), s.ID)
	return plan, nil
}

func accountAllowed(accounts []string, profile string) bool {
	for _, a := range accounts {
		if strings.EqualFold(strings.TrimSpace(a), profile) {
			return true
		}
	}
	return false
}

// claudeConfigDir is the directory a CLAUDE_CONFIG_DIR value names, or the
// default ~/.claude for "".
func claudeConfigDir(configDir string) string {
	if configDir != "" {
		return configDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func findBoardSession(st sessions.State, ref string) (sessions.Session, error) {
	ref = strings.TrimSpace(ref)
	for _, s := range st.Sessions {
		if s.ID == ref {
			return s, nil
		}
	}
	if n, err := keyIndex(ref); err == nil {
		for _, sl := range st.Slots {
			if sl.Index == n && sl.SessionID != "" {
				return findBoardSession(st, sl.SessionID)
			}
		}
		return sessions.Session{}, fmt.Errorf("key %s is empty", ref)
	}
	var matches []sessions.Session
	for _, s := range st.Sessions {
		if strings.HasPrefix(s.ID, ref) || strings.EqualFold(s.Label, ref) || strings.EqualFold(s.Display, ref) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return sessions.Session{}, fmt.Errorf("no session matches %q", ref)
	}
	return sessions.Session{}, fmt.Errorf("%q matches %d sessions — use the id", ref, len(matches))
}

func init() {
	agentCarryCmd.Flags().String("profile", "", "The account (corgi profile) to continue under")
	agentCmd.AddCommand(agentCarryCmd)
}
