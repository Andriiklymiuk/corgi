package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

// Blocked is a column, not a failure: the ticket waits for a person, and no
// unattended run touches it until someone says the wall is gone.
var agentWatchBlockCmd = &cobra.Command{
	Use:   "block <REF> <reason>",
	Short: "Keep unattended runs off a ticket until you unblock it",
	Long: `Marks a ticket blocked: the inbox shows the reason, the workpad comment
carries it, and the unattended mode leaves it alone. corgi does this itself
after two failed runs in a row, or when a run's handoff says it is blocked on
a missing tool, credential or secret.

  corgi agent watch block ABC-123 "needs the payments sandbox key"
  corgi agent watch unblock ABC-123`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			exitWithError("agent_watch_block", err, 2)
		}
		ref := strings.TrimSpace(args[0])
		watch.LoadFixLog(dir).Block(id, ref, args[1], watch.BlockedByPerson, time.Now())
		if _, _, err := watchWriter(dir, id); err == nil {
			writeWorkpad(dir, id, ref, "Blocked", strings.TrimSpace(args[1])+"\n\n`corgi agent watch unblock "+ref+"` once it is fixed.")
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": id, "ref": ref, "blocked": true, "reason": args[1]})
			return
		}
		fmt.Printf("%s blocked: %s\n", ref, args[1])
	},
}

var agentWatchUnblockCmd = &cobra.Command{
	Use:   "unblock <REF>",
	Short: "Let unattended runs work a blocked ticket again",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			exitWithError("agent_watch_unblock", err, 2)
		}
		ref := strings.TrimSpace(args[0])
		if !watch.LoadFixLog(dir).Unblock(id, ref) {
			exitWithError("agent_watch_unblock", fmt.Errorf("%s is not blocked in %s", ref, id), 2)
		}
		if _, _, err := watchWriter(dir, id); err == nil {
			writeWorkpad(dir, id, ref, "Blocked", "")
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": id, "ref": ref, "blocked": false})
			return
		}
		fmt.Printf("%s unblocked — the next matching event runs\n", ref)
	},
}

func init() {
	for _, c := range []*cobra.Command{agentWatchBlockCmd, agentWatchUnblockCmd} {
		c.Flags().String("workspace", "", "the workspace the ticket belongs to (default: the one you are in)")
	}
	agentWatchCmd.AddCommand(agentWatchBlockCmd, agentWatchUnblockCmd)
}
