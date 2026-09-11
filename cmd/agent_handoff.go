package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

// A handoff is what one run leaves for the next: typed, short, never a
// transcript. Written by the session that is stopping — on a limit, at the
// end of the day, before a carry — and read by whatever picks the ticket up,
// on this machine or another, under any account.
var agentHandoffCmd = &cobra.Command{
	Use:   "handoff [--ref ABC-123] --done … --remaining … --next …",
	Short: "Leave a typed handoff for the next run on this ticket",
	Long: `Writes .corgi/corgi_services/handoffs/<ref>.json and a Markdown twin in the
workspace. corgi fills in where the code is (branch, head, base, uncommitted
files), who is writing (session, account) and how much budget was left; you
say what is done, what is not, what was decided and what to ask.

  corgi agent handoff --ref ABC-123 \
    --done "POST /limits returns 429" --done "regression test" \
    --remaining "web banner on 429" \
    --decision "5h sliding window, not fixed" \
    --uncertain "should the phone retry by itself?" \
    --next "web banner, then open the web PR" \
    --verify "corgi test --changed"

  corgi agent handoff show ABC-123      # the Markdown twin
  corgi agent handoff list              # every packet here, newest first
  corgi agent handoff verify ABC-123    # re-run its check at the current head
  corgi agent handoff rm ABC-123

--ref defaults to the ticket key in the branch name. --verify runs the command
now and records its exit code and the head it ran on; the next run re-runs it
before trusting the packet. A packet that names a secret or a TODO is refused.`,
	Args: cobra.NoArgs,
	Run:  runAgentHandoff,
}

var agentHandoffShowCmd = &cobra.Command{
	Use:   "show <ref>",
	Short: "Print a handoff",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := handoffWorkspaceDir(cmd)
		p, err := handoff.Read(dir, args[0])
		if err != nil {
			exitWithError("agent_handoff_show", fmt.Errorf("no handoff for %s in %s", args[0], dir), 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(p)
			return
		}
		fmt.Print(p.Markdown())
		if n, err := handoff.CommitsSince(worktreeFor(dir, p), p.Where.Head); err == nil && n > 0 {
			fmt.Printf("\n%d commit(s) since it was written — the code moved on; read the diff too.\n", n)
		}
	},
}

var agentHandoffListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every handoff in this workspace, newest first",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := handoffWorkspaceDir(cmd)
		list := handoff.List(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": dir, "handoffs": list})
			return
		}
		if len(list) == 0 {
			fmt.Printf("No handoffs in %s\n", handoff.Dir(dir))
			return
		}
		for _, p := range list {
			fmt.Printf("%-12s %-16s %s  (%s)\n", p.Ref, p.State, p.Summary(), roughAge(time.Since(p.WrittenAt)))
		}
	},
}

var agentHandoffVerifyCmd = &cobra.Command{
	Use:   "verify <ref>",
	Short: "Re-run a handoff's check at the current head",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := handoffWorkspaceDir(cmd)
		p, err := handoff.Read(dir, args[0])
		if err != nil {
			exitWithError("agent_handoff_verify", fmt.Errorf("no handoff for %s in %s", args[0], dir), 2)
		}
		if p.Verification == nil {
			exitWithError("agent_handoff_verify", fmt.Errorf("%s has no verification command", p.Ref), 2)
		}
		v, ok := handoff.Verify(worktreeFor(dir, p), p, runShell)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ref": p.Ref, "verified": ok, "result": v, "recorded": p.Verification})
		} else if ok {
			fmt.Printf("✓ %s: `%s` passes at %s — the packet can be trusted as written\n", p.Ref, v.Cmd, shortSHA(v.At))
		} else {
			fmt.Printf("✗ %s: `%s` exit %d at %s (packet said exit %d at %s) — start from the ticket and the diff, not the packet\n",
				p.Ref, v.Cmd, v.Exit, shortSHA(v.At), p.Verification.Exit, shortSHA(p.Verification.At))
		}
		if !ok {
			os.Exit(1)
		}
	},
}

var agentHandoffRmCmd = &cobra.Command{
	Use:   "rm <ref>",
	Short: "Delete a handoff",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := handoffWorkspaceDir(cmd)
		if err := handoff.Remove(dir, args[0]); err != nil {
			exitWithError("agent_handoff_rm", err, 1)
		}
		fmt.Printf("removed %s\n", args[0])
	},
}

func runAgentHandoff(cmd *cobra.Command, _ []string) {
	f := cmd.Flags()
	dir := handoffWorkspaceDir(cmd)
	cwd, _ := os.Getwd()
	where := handoff.GitWhere(cwd)
	ref, _ := f.GetString("ref")
	if ref == "" {
		ref = handoff.RefFromBranch(where.Branch)
	}
	if ref == "" {
		exitWithError("agent_handoff", fmt.Errorf("say --ref: the branch %q names no ticket", where.Branch), 2)
	}
	if rel, err := filepath.Rel(dir, cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		where.Worktree = rel
	}
	state, _ := f.GetString("state")
	blocked, _ := f.GetString("blocked")
	if blocked != "" {
		state = handoff.StateBlocked
	}
	done, _ := f.GetStringArray("done")
	remaining, _ := f.GetStringArray("remaining")
	decisions, _ := f.GetStringArray("decision")
	uncertain, _ := f.GetStringArray("uncertain")
	next, _ := f.GetString("next")
	tracker, _ := f.GetString("tracker")
	model, _ := f.GetString("model")
	sessionRef, _ := f.GetString("session")

	p := handoff.Packet{Ref: strings.ToUpper(strings.TrimSpace(ref)), Tracker: tracker, State: state, Blocked: blocked,
		Where: where, Done: done, Remaining: remaining, Decisions: decisions, Uncertain: uncertain, Next: next,
		From: handoff.From{Harness: "claude", Model: model, Host: hostname()}}
	fillFromBoard(&p, sessionRef, cwd)

	if verify, _ := f.GetString("verify"); verify != "" {
		code, _ := runShell(cwd, verify)
		p.Verification = &handoff.Verification{Cmd: verify, Exit: code, At: where.Head, RanAt: time.Now()}
	}
	if err := handoff.Write(dir, p); err != nil {
		exitWithError("agent_handoff", err, 2)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"path": handoff.Path(dir, p.Ref), "markdown": handoff.MarkdownPath(dir, p.Ref), "handoff": p})
		return
	}
	fmt.Printf("✓ handoff for %s: %s\n  %s\n", p.Ref, p.Summary(), handoff.MarkdownPath(dir, p.Ref))
	if p.Verification != nil && p.Verification.Exit != 0 {
		fmt.Printf("  note: `%s` exited %d — the next run will see that\n", p.Verification.Cmd, p.Verification.Exit)
	}
}

// fillFromBoard adds who is writing and how much room was left, from the
// session board: the session named, else the one whose cwd this is.
func fillFromBoard(p *handoff.Packet, sessionRef, cwd string) {
	dir := agentDirOrEmpty()
	if dir == "" {
		return
	}
	rep, err := readBoard(dir)
	if err != nil {
		return
	}
	var match *sessions.Session
	for i := range rep.State.Sessions {
		s := &rep.State.Sessions[i]
		if sessionRef != "" && (s.ID == sessionRef || s.Label == sessionRef || s.Display == sessionRef) {
			match = s
			break
		}
		if sessionRef == "" && s.Cwd == cwd && s.Status != sessions.StatusGone {
			match = s
		}
	}
	if match == nil {
		return
	}
	p.From.Session = match.ID
	p.From.Account = match.Profile
	if p.From.Model == "" && match.Context != nil {
		p.From.Model = match.Context.Model
	}
	if match.Context != nil {
		p.Budget.Context = match.Context.Percent
	}
	if l, ok := usage.ReadLimits(match.ConfigDir); ok {
		p.Budget.FiveHour, p.Budget.SevenDay = l.FiveHour.Percent, l.SevenDay.Percent
	}
}

// handoffWorkspaceDir is where the packets live: the registered workspace
// that contains cwd, else the git root, else cwd. --dir overrides.
func handoffWorkspaceDir(cmd *cobra.Command) string {
	if d, _ := cmd.Flags().GetString("dir"); d != "" {
		return d
	}
	cwd, _ := os.Getwd()
	if dir := agentDirOrEmpty(); dir != "" {
		if registry, err := workspace.Load(agentRegistryPath(dir)); err == nil {
			if _, root := workspaceLabel(registry, cwd); root != "" {
				return root
			}
		}
	}
	if root := sessions.RepoRoot(cwd); root != "" {
		return root
	}
	return cwd
}

func worktreeFor(dir string, p handoff.Packet) string {
	if p.Where.Worktree != "" {
		return filepath.Join(dir, p.Where.Worktree)
	}
	return dir
}

func runShell(dir, command string) (int, error) {
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), err
	}
	return 1, err
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func init() {
	f := agentHandoffCmd.Flags()
	f.String("ref", "", "the ticket (default: the key in the branch name)")
	f.String("tracker", "", "the ticket's URL")
	f.String("state", handoff.StateInputRequired, "working, input-required, auth-required, completed, failed")
	f.String("blocked", "", "the missing tool, credential or secret (sets state blocked)")
	f.StringArray("done", nil, "one thing that is finished (repeatable)")
	f.StringArray("remaining", nil, "one thing that is not (repeatable)")
	f.StringArray("decision", nil, "one call made, so the next run does not re-derive it (repeatable)")
	f.StringArray("uncertain", nil, "one thing to ask a person before assuming (repeatable)")
	f.String("next", "", "the first thing the next run should do")
	f.String("verify", "", "a command to run now and record, e.g. \"corgi test --changed\"")
	f.String("model", "", "the model that did the work")
	f.String("session", "", "the board session writing this (default: the one in this directory)")
	for _, c := range []*cobra.Command{agentHandoffCmd, agentHandoffShowCmd, agentHandoffListCmd, agentHandoffVerifyCmd, agentHandoffRmCmd} {
		c.Flags().String("dir", "", "the workspace (default: the one containing the current directory)")
	}
	agentHandoffCmd.AddCommand(agentHandoffShowCmd, agentHandoffListCmd, agentHandoffVerifyCmd, agentHandoffRmCmd)
	agentCmd.AddCommand(agentHandoffCmd)
}
