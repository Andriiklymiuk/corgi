package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

var agentWatchPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove the worktrees of finished isolated runs",
	Long: `An isolated run leaves its worktrees behind so undo has something to
remove and a follow-up lands in the same place. On a machine that runs
unattended they add up. This removes the worktrees of runs finished more
than --older-than ago; the branch stays, and a worktree with uncommitted
work stays and is named.

  corgi agent watch prune                    # runs finished more than 7 days ago
  corgi agent watch prune --older-than 2d
  corgi agent watch prune --dry-run

The daemon does the same on its own for a workspace enabled with
--prune-after.`,
	Run: runAgentWatchPrune,
}

type pruneRow struct {
	Workspace string   `json:"workspace"`
	Branch    string   `json:"branch"`
	Removed   []string `json:"removed,omitempty"`
	Kept      []string `json:"kept,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func runAgentWatchPrune(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	dry, _ := cmd.Flags().GetBool(watchFlagDryRun)
	ageFlag, _ := cmd.Flags().GetString("older-than")
	age, err := watch.ParseAge(ageFlag)
	if err != nil {
		exitWithError("agent_watch_prune", err, 2)
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		exitWithError("agent_watch_prune", err, 1)
	}
	var rows []pruneRow
	for _, r := range watch.PrunableFixes(watch.LoadFixLog(dir).RecentFixes("", 500), age, time.Now()) {
		if ws, ok := registry.Find(r.Workspace); ok {
			if row, keep := pruneRun(ws.AbsPath, r, dry); keep {
				rows = append(rows, row)
			}
		}
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"olderThan": ageFlag, "dryRun": dry, "runs": rows})
		return
	}
	if len(rows) == 0 {
		fmt.Println("nothing to prune")
		return
	}
	for _, r := range rows {
		printPruneRow(r, dry)
	}
}

func pruneRun(root string, r watch.FixRecord, dry bool) (pruneRow, bool) {
	out := pruneRow{Workspace: r.Workspace, Branch: r.Branch}
	if dry {
		return out, true
	}
	var err error
	out.Removed, out.Kept, err = releaseFixWorktrees(root, r.Branch)
	if err != nil {
		out.Error = err.Error()
	}
	return out, len(out.Removed) > 0 || len(out.Kept) > 0 || out.Error != ""
}

func printPruneRow(r pruneRow, dry bool) {
	switch {
	case dry:
		fmt.Printf("would prune %s (%s)\n", r.Branch, r.Workspace)
	case r.Error != "":
		fmt.Printf("%s (%s): %s\n", r.Branch, r.Workspace, r.Error)
	default:
		for _, p := range r.Removed {
			fmt.Printf("removed %s\n", p)
		}
		for _, p := range r.Kept {
			fmt.Printf("kept    %s - uncommitted work\n", p)
		}
	}
}

func init() {
	agentWatchPruneCmd.Flags().String("older-than", "7d", "Only runs finished at least this long ago, e.g. 7d or 48h")
	agentWatchPruneCmd.Flags().Bool(watchFlagDryRun, false, "Say what it would remove and change nothing")
	agentWatchCmd.AddCommand(agentWatchPruneCmd)
}
