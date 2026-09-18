package cmd

import (
	"errors"
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
the new one is up; its process is left alone.

--fork N opens N new sessions that all continue this one's conversation (claude
--fork-session), each in its own terminal: one session that read the code once
becomes several that build on the same context. --prompt, repeated, hands each
fork its own first message, in order; forks past the last --prompt start with
none. Without --profile the forks run under the session's own account. The
original keeps running.

  corgi agent carry k3 --fork 3 --prompt "do the api side" --prompt "do the web side" --prompt "write the tests"`,
	Args: cobra.ExactArgs(1),
	Run:  runAgentCarry,
}

func runAgentCarry(cmd *cobra.Command, args []string) {
	profile, _ := cmd.Flags().GetString("profile")
	profile = strings.TrimSpace(profile)
	fresh, _ := cmd.Flags().GetBool("fresh")
	forks, _ := cmd.Flags().GetInt("fork")
	prompts, _ := cmd.Flags().GetStringArray("prompt")
	dir := mustAgentDir()
	board, err := readBoard(dir)
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	s, err := findBoardSession(board.State, args[0])
	if err != nil {
		exitWithError("agent_carry", err, 1)
	}
	if forks > 0 || len(prompts) > 0 {
		if fresh {
			exitWithError("agent_carry", fmt.Errorf("--fork continues the conversation; it cannot be combined with --fresh"), 2)
		}
		if err := forkSession(dir, s, profile, forks, prompts, "cli"); err != nil {
			code := 1
			var ce *carryError
			if errors.As(err, &ce) {
				code = ce.code
			}
			exitWithError("agent_carry", err, code)
		}
		if !utils.JSONOutput {
			utils.Infof("%d forks of %s are opening; the original keeps running\n", forks, s.Display)
		}
		return
	}
	if profile == "" && fresh {
		profile = firstNonEmpty(s.Profile, "default")
	}
	if profile == "" {
		exitWithError("agent_carry", fmt.Errorf("--profile is required: which account to carry to (or --fresh to restart under the same one)"), 2)
	}
	packetPath, err := carrySession(dir, s, profile, fresh, "cli")
	if err != nil {
		code := 1
		var ce *carryError
		if errors.As(err, &ce) {
			code = ce.code
		}
		exitWithError("agent_carry", err, code)
	}
	if packetPath != "" && !utils.JSONOutput {
		utils.Infof("handoff: %s\n", packetPath)
	}
	if !utils.JSONOutput {
		utils.Info("the old session keeps running; close its tab when you are done with it")
	}
}

type carryError struct {
	err  error
	code int
}

func (e *carryError) Error() string { return e.err.Error() }
func (e *carryError) Unwrap() error { return e.err }

func carrySession(dir string, s sessions.Session, profile string, fresh bool, source string) (packetPath string, err error) {
	if !fresh && s.Context != nil && s.Context.Percent >= carryFreshAt {
		fresh = true
		utils.Infof("context is %d%% full — starting the new session clean, from the handoff\n", s.Context.Percent)
	}
	plan, err := planCarry(dir, s, profile, fresh)
	if err != nil {
		return "", &carryError{err, 1}
	}
	packet, packetPath := leaveCarryHandoff(plan.Workspace, s, profile)
	if fresh {
		if packetPath == "" {
			return "", &carryError{fmt.Errorf("a fresh start needs a handoff, and the branch %q names no ticket — say --ref on `corgi agent handoff` first", sessions.Branch(s.Cwd)), 2}
		}
		id, err := savePrompt(dir, carryPrompt(packet, packetPath))
		if err != nil {
			return "", &carryError{err, 1}
		}
		plan.Command = fmt.Sprintf("%s agent claude --profile %s --prompt-id %s", shellQuote(plan.Exe), shellQuote(profile), id)
	} else {
		if err := os.MkdirAll(filepath.Dir(plan.To), 0o700); err != nil {
			return "", &carryError{err, 1}
		}
		if err := copyFile(plan.From, plan.To); err != nil {
			return "", &carryError{fmt.Errorf("copy transcript: %w", err), 1}
		}
	}
	how := "a new terminal resumes it there"
	if fresh {
		how = "a new terminal starts clean there, from the handoff"
	}
	sendBoardCommand(command.Command{Action: command.ActionNew, WindowID: s.Host.WindowID, Command: plan.Command, Source: source},
		fmt.Sprintf("carrying %s to %s: %s", s.Display, profile, how))
	if s.Status == sessions.StatusLimited || s.Status == sessions.StatusDone || s.Status == sessions.StatusStale {
		_, _ = command.Write(dir, command.Command{Action: command.ActionDismiss, SessionID: s.ID, Source: source})
	}
	return packetPath, nil
}

const maxForks = 8

func forkSession(dir string, s sessions.Session, profile string, n int, prompts []string, source string) error {
	if n < 2 || n > maxForks {
		return &carryError{fmt.Errorf("--fork takes 2 to %d", maxForks), 2}
	}
	if len(prompts) > n {
		return &carryError{fmt.Errorf("%d prompts for %d forks — one --prompt per fork at most", len(prompts), n), 2}
	}
	own := firstNonEmpty(s.Profile, "default")
	if profile == "" {
		profile = own
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "corgi"
	}
	if !strings.EqualFold(profile, own) {
		plan, err := planCarry(dir, s, profile, false)
		if err != nil {
			return &carryError{err, 1}
		}
		if err := os.MkdirAll(filepath.Dir(plan.To), 0o700); err != nil {
			return &carryError{err, 1}
		}
		if err := copyFile(plan.From, plan.To); err != nil {
			return &carryError{fmt.Errorf("copy transcript: %w", err), 1}
		}
		exe = plan.Exe
	} else if s.Cwd == "" || sessions.Placeholder(s.ID) {
		return &carryError{fmt.Errorf("%s has not reported its session id yet", s.Display), 1}
	}
	ids := make([]string, 0, len(prompts))
	for _, text := range prompts {
		id, err := savePrompt(dir, text)
		if err != nil {
			return &carryError{err, 1}
		}
		ids = append(ids, id)
	}
	for i, c := range forkCommands(exe, profile, s.ID, ids, n) {
		sendBoardCommand(command.Command{Action: command.ActionNew, WindowID: s.Host.WindowID, Command: c, Source: source},
			fmt.Sprintf("fork %d of %s opens under %s", i+1, s.Display, profile))
	}
	return nil
}

func forkCommands(exe, profile, sessionID string, promptIDs []string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		c := fmt.Sprintf("%s agent claude --profile %s", shellQuote(exe), shellQuote(profile))
		if i < len(promptIDs) {
			c += " --prompt-id " + shellQuote(promptIDs[i])
		}
		out = append(out, c+fmt.Sprintf(" -- --resume %s --fork-session", shellQuote(sessionID)))
	}
	return out
}

func carryProfileFor(dir string, s sessions.Session) (string, error) {
	if s.Cwd == "" || sessions.Placeholder(s.ID) {
		return "", nil
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil {
		return "", err
	}
	launch, err := resolveClaudeLaunch(s.Cwd, "", nil)
	if err != nil || launch.Workspace == "" {
		return "", err
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return "", err
	}
	ws, ok := registry.Find(launch.Workspace)
	if !ok {
		return "", nil
	}
	repo, _ := config.LoadRepo(ws.AbsPath)
	resolved := config.Resolve(launch.Workspace, repo, user)
	current := firstNonEmpty(s.Profile, "default")
	best, bestLeft := "", 0
	for _, name := range resolved.Accounts {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, current) {
			continue
		}
		target, err := config.ApplyProfile(resolved, user, name)
		if err != nil {
			continue
		}
		limits, ok := usage.ReadLimits(expandTilde(target.ConfigDir))
		if !ok {
			continue
		}
		left := 100 - limits.FiveHour.Percent
		if limits.SevenDay.Percent >= quotaFullAt || limits.FiveHour.Percent >= quotaFullAt {
			continue
		}
		if left > bestLeft {
			best, bestLeft = name, left
		}
	}
	return best, nil
}

const quotaFullAt = 95

func autoCarry(dir string) func(s sessions.Session) (string, error) {
	return func(s sessions.Session) (string, error) {
		profile, err := carryProfileFor(dir, s)
		if err != nil || profile == "" {
			return "", err
		}
		if _, err := carrySession(dir, s, profile, false, "daemon"); err != nil {
			return "", err
		}
		return profile, nil
	}
}

type carryPlan struct {
	From, To  string
	Command   string
	Exe       string
	Workspace string
}

const carryFreshAt = 85

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

func planCarry(agentD string, s sessions.Session, profile string, fresh bool) (carryPlan, error) {
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
	sameAccount := strings.EqualFold(profile, firstNonEmpty(s.Profile, "default"))
	if !sameAccount && !accountAllowed(resolved.Accounts, profile) {
		if len(resolved.Accounts) == 0 {
			return carryPlan{}, fmt.Errorf("workspace %s lists no accounts: add `accounts: [default, %s]` under it in %s", launch.Workspace, profile, agentUserConfigPath(agentD))
		}
		return carryPlan{}, fmt.Errorf("workspace %s may run under %s, not %s", launch.Workspace, strings.Join(resolved.Accounts, ", "), profile)
	}
	target := resolved
	if !sameAccount || profile != "default" {
		if target, err = config.ApplyProfile(resolved, user, profile); err != nil {
			return carryPlan{}, err
		}
	}
	fromDir := claudeConfigDir(s.ConfigDir)
	toDir := claudeConfigDir(expandTilde(target.ConfigDir))
	if samePath(fromDir, toDir) && !fresh {
		return carryPlan{}, fmt.Errorf("%s already runs under %s — --fresh restarts it there from a handoff", s.Display, profile)
	}
	rel := filepath.Join("projects", mungeClaudeProjectDir(s.Cwd), s.ID+".jsonl")
	plan := carryPlan{From: filepath.Join(fromDir, rel), To: filepath.Join(toDir, rel)}
	if _, err := os.Stat(plan.From); err != nil && !fresh {
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
	agentCarryCmd.Flags().Int("fork", 0, "Open this many sessions that all continue the conversation (2 to 8); the original keeps running")
	agentCarryCmd.Flags().StringArray("prompt", nil, "First message for the next fork, in order; repeat once per fork")
	agentCarryCmd.Flags().Bool("fresh", false, "Start clean from the handoff instead of resuming the transcript (automatic past 85% context); without --profile, the same account")
	agentCmd.AddCommand(agentCarryCmd)
}
