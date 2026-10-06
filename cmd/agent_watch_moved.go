package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var agentWatchMovedCmd = &cobra.Command{
	Use:   "moved",
	Short: "Tickets that changed column, read with corgi's own tracker token",
	Long: `Every column change on the workspace's tracker since a moment, newest first:
the ticket, from, to, and who moved it. It reads with the token corgi already
holds, so a digest has ticket moves even when the tracker's MCP wants a sign-in.

  corgi agent watch moved                    # the last 24 hours
  corgi agent watch moved --since 72h        # a long weekend
  corgi agent watch moved --workspace api --json`,
	Run: runAgentWatchMoved,
}

func runAgentWatchMoved(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	id, err := watchTargetWorkspace(dir, cmd.Flags())
	if err != nil {
		exitWithError("agent_watch_moved", err, 2)
	}
	sinceText, _ := cmd.Flags().GetString("since")
	window, err := time.ParseDuration(sinceText)
	if err != nil || window <= 0 {
		exitWithError("agent_watch_moved", fmt.Errorf("--since is a duration like 24h or 72h, not %q", sinceText), 2)
	}
	w, _, err := watchWriter(dir, id)
	if err != nil {
		exitWithError("agent_watch_moved", err, 2)
	}
	mover, ok := w.(watch.Mover)
	if !ok {
		exitWithError("agent_watch_moved", fmt.Errorf("%s cannot list column changes", w.Name()), 2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moves, err := mover.Moves(ctx, time.Now().Add(-window))
	cut := errors.Is(err, watch.ErrMovesCut)
	if err != nil && !cut {
		exitWithError("agent_watch_moved", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"workspace": id, "since": sinceText, "moves": moves, "cut": cut})
		return
	}
	fmt.Print(movesText(moves, sinceText, cut))
}

func movesText(moves []watch.Move, since string, cut bool) string {
	if len(moves) == 0 {
		return "no ticket changed column in the last " + since + "\n"
	}
	out := ""
	for _, m := range moves {
		from := m.From
		if from == "" {
			from = "?"
		}
		line := fmt.Sprintf("%s  %s  %s → %s", m.At.Local().Format("01-02 15:04"), m.Ref, from, m.To)
		if m.By != "" {
			line += "  by " + m.By
		}
		out += line + "  · " + m.Title + "\n"
	}
	if cut {
		out += watch.ErrMovesCut.Error() + "\n"
	}
	return out
}

func init() {
	f := agentWatchMovedCmd.Flags()
	f.String("since", "24h", "How far back to look")
	f.String("workspace", "", "Workspace id; omitted means the one you are in")
	agentWatchCmd.AddCommand(agentWatchMovedCmd)
}
