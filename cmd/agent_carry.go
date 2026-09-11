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
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

var agentCarryCmd = &cobra.Command{
	Use:   "carry <session> --profile <name>",
	Short: "Continue a session under another account, conversation and all",
	Long: `Moves a session to another of the workspace's accounts. For the session that
hit its limit while the other account still has budget.

First it leaves a handoff for the ticket (corgi agent handoff): a draft from
git and the last thing the session said, unless the session wrote its own.
Then, by default, the transcript is copied into the other account's config
directory and a new terminal in the same window runs
` + "`corgi agent claude --profile <name> -- --resume <id>`" + `, so the conversation
carries on where it was. With --fresh — or on its own when the context window
is over 85% full — the new session starts clean instead, with the handoff as
its first prompt: same worktree, none of the old context.

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
	fresh, _ := cmd.Flags().GetBool("fresh")
	if !fresh && s.Context != nil && s.Context.Percent >= carryFreshAt {
		fresh = true
		utils.Infof("context is %d%% full — starting the new session clean, from the handoff\n", s.Context.Percent)
	}
	plan, err := planCarry(dir, s, profile)
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	packet, packetPath := leaveCarryHandoff(plan.Workspace, s, profile)
	if fresh {
		if packetPath == "" {
			exitWithError("agent_carry", fmt.Errorf("a fresh start needs a handoff, and the branch %q names no ticket — say --ref on `corgi agent handoff` first", sessions.Branch(s.Cwd)), 2)
		}
		id, err := savePrompt(dir, carryPrompt(packet, packetPath))
		if err != nil {
			exitWithError("agent_carry", err, 1)
		}
		plan.Command = fmt.Sprintf("%s agent claude --profile %s --prompt-id %s", shellQuote(plan.Exe), shellQuote(profile), id)
	} else {
		if err := os.MkdirAll(filepath.Dir(plan.To), 0o700); err != nil {
			exitWithError("agent_carry", err, 1)
		}
		if err := copyFile(plan.From, plan.To); err != nil {
			exitWithError("agent_carry", fmt.Errorf("copy transcript: %w", err), 1)
		}
	}
	how := "a new terminal resumes it there"
	if fresh {
		how = "a new terminal starts clean there, from the handoff"
	}
	sendBoardCommand(command.Command{Action: command.ActionNew, WindowID: s.Host.WindowID, Command: plan.Command, Source: "cli"},
		fmt.Sprintf("carrying %s to %s: %s", s.Display, profile, how))
	if packetPath != "" && !utils.JSONOutput {
		utils.Infof("handoff: %s\n", packetPath)
	}
	if s.Status == sessions.StatusLimited || s.Status == sessions.StatusDone || s.Status == sessions.StatusStale {
		_, _ = command.Write(dir, command.Command{Action: command.ActionDismiss, SessionID: s.ID, Source: "cli"})
	}
	if !utils.JSONOutput {
		utils.Info("the old session keeps running; close its tab when you are done with it")
	}
}

type carryPlan struct {
	From, To  string
	Command   string
	Exe       string
	Workspace string
}

// carryFreshAt is the context fill past which resuming the transcript
// carries mostly baggage; the handoff is the better start.
const carryFreshAt = 85

// leaveCarryHandoff makes sure the ticket has a handoff before the move: the
// session's own if it wrote one after its last turn began, else a draft from
// git and the last thing it said. Returns the packet and its Markdown path,
// or "" when the branch names no ticket.
func leaveCarryHandoff(workspaceDir string, s sessions.Session, profile string) (handoff.Packet, string) {
	if workspaceDir == "" {
		return handoff.Packet{}, ""
	}
	where := handoff.GitWhere(s.Cwd)
	ref := handoff.RefFromBranch(where.Branch)
	if ref == "" {
		return handoff.Packet{}, ""
	}
	if p, err := handoff.Read(workspaceDir, ref); err == nil && !p.Draft && !p.WrittenAt.Before(s.TurnStartedAt) {
		return p, handoff.MarkdownPath(workspaceDir, ref)
	}
	if rel, err := filepath.Rel(workspaceDir, s.Cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		where.Worktree = rel
	}
	p := handoff.Packet{Ref: ref, State: handoff.StateInputRequired, Where: where, Draft: true,
		Next: firstLineOf(s.Summary),
		From: handoff.From{Harness: "claude", Session: s.ID, Account: s.Profile, Host: hostname()}}
	if s.Context != nil {
		p.From.Model = s.Context.Model
		p.Budget.Context = s.Context.Percent
	}
	if l, ok := usage.ReadLimits(s.ConfigDir); ok {
		p.Budget.FiveHour, p.Budget.SevenDay = l.FiveHour.Percent, l.SevenDay.Percent
	}
	if s.Status == sessions.StatusLimited {
		p.Decisions = []string{fmt.Sprintf("carried from %s to %s: the first account hit its limit", firstNonEmpty(s.Profile, "default"), profile)}
	}
	if err := handoff.Write(workspaceDir, p); err != nil {
		utils.Infof("could not write a handoff: %v\n", err)
		return handoff.Packet{}, ""
	}
	return p, handoff.MarkdownPath(workspaceDir, ref)
}

// carryPrompt is the first message of a fresh session: read the packet,
// then do the next thing.
func carryPrompt(p handoff.Packet, path string) string {
	msg := fmt.Sprintf("Continue the work on %s. Read the handoff first: %s — it is typed state from the previous session, not a transcript; check its done list against the diff before building on it.", p.Ref, path)
	if p.Draft {
		msg += " It is a draft corgi assembled from git and the last thing the session said, so start by looking at the branch."
	}
	if p.Next != "" {
		msg += " Then: " + p.Next
	}
	return msg
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
	plan.Exe, plan.Workspace = exe, ws.AbsPath
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
	agentCarryCmd.Flags().Bool("fresh", false, "Start clean from the handoff instead of resuming the transcript (automatic past 85% context)")
	agentCmd.AddCommand(agentCarryCmd)
}
