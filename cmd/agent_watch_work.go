package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentWatchWorkCmd = &cobra.Command{
	Use:   "work <REF|key> [REF...]",
	Short: "Open a Claude session on a ticket, with the prompt an unattended run would get",
	Long: `Hands an inbox row to a real session: the same prompt the daemon's own fix
would have used, opened as a chat in the editor window on that workspace, so
you can watch and steer. What the page's "Work on it" and the phone do.

  corgi agent watch work ABC-123
  corgi agent watch work ABC-123 ABC-124            one session, both stories
  corgi agent watch work ABC-123 --model opus --profile work
  corgi agent watch work ABC-123 --attempts 3 --models opus,sonnet    three sessions, a worktree each; corgi agent attempts compares them

A REF is what the inbox shows; a key (linear:ABC-123:comment:…) is what --json
prints. A ref names its newest row. The daemon must be running.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		workspace, _ := cmd.Flags().GetString("workspace")
		var keys []string
		for _, arg := range args {
			found := inboxKeysFor(dir, arg, workspace)
			if len(found) == 0 {
				exitWithError("agent_watch_work", fmt.Errorf("nothing in the inbox matches %q", arg), 2)
			}
			keys = append(keys, newestIssueKey(dir, found))
		}
		window, _ := cmd.Flags().GetString("window")
		model, _ := cmd.Flags().GetString("model")
		profile, _ := cmd.Flags().GetString("profile")
		from, _ := cmd.Flags().GetString("from")
		isolate, _ := cmd.Flags().GetBool("isolate")
		attempts, _ := cmd.Flags().GetInt("attempts")
		modelList, _ := cmd.Flags().GetString("models")
		if from != "editor" && from != "bar" {
			from = "cli"
		}
		cmds, status, msg := workOnCommands(dir, keys, workOnOptions{Window: window, Model: model, Profile: profile, Source: from, Isolate: isolate}, attempts, splitList(modelList))
		if status != 0 {
			exitWithError("agent_watch_work", fmt.Errorf("%s", msg), 1)
		}
		for i, c := range cmds {
			done := fmt.Sprintf("asked the editor for a session on %s - `corgi agent sessions` in a moment", strings.Join(args, ", "))
			if len(cmds) > 1 {
				done = fmt.Sprintf("asked the editor for attempt %d of %d on %s - `corgi agent attempts` compares them", i+1, len(cmds), strings.Join(args, ", "))
			}
			sendBoardCommand(c, done)
		}
	},
}

func newestIssueKey(dir string, keys []string) string {
	for _, k := range keys {
		if e, ok := watch.FindEvent(dir, k); ok && strings.HasPrefix(string(e.Kind), "issue.") {
			return k
		}
	}
	return keys[0]
}

func init() {
	agentWatchWorkCmd.Flags().String("workspace", "", "only this workspace's rows, when a ref exists in two")
	agentWatchWorkCmd.Flags().String("window", "", "the editor window id (corgi agent windows); default: the one on that workspace, else the front one")
	agentWatchWorkCmd.Flags().String("model", "", "claude model for the session")
	agentWatchWorkCmd.Flags().String("profile", "", "corgi profile (account) for the session")
	agentWatchWorkCmd.Flags().String("from", "", "who pressed it, for the board: editor or bar")
	agentWatchWorkCmd.Flags().Int("attempts", 0, "Open this many sessions on the ticket to compare (at most 5), a worktree each on corgi/<ref>-N; corgi agent attempts lists them and picks one")
	agentWatchWorkCmd.Flags().String("models", "", "The models the attempts run on, in turn (comma separated) - opus,sonnet,haiku; with one, all use it")
	agentWatchWorkCmd.Flags().Bool("isolate", false, "a worktree of the session's own on corgi/<ref>, one per repository - it never touches your checkout")
	_ = agentWatchWorkCmd.Flags().MarkHidden("from")
	agentWatchCmd.AddCommand(agentWatchWorkCmd)
}
