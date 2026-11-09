package cmd

import (
	"fmt"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

const (
	reconcileKeep      = 7 * 24 * time.Hour
	reconcileUntouched = 2 * reconcileKeep
)

// reconcileKanban decides which cards are over and archives them: Done for
// more than a day, a run that answered a week ago with nothing since, a
// review whose pull request is closed, a card untouched for two weeks. It
// never touches a card with a session, a run or an open pull request on it.
func reconcileKanban(cards []KanbanCard, archive *watch.ArchiveLog, keep time.Duration, now time.Time, dry bool) []string {
	if keep <= 0 {
		keep = reconcileKeep
	}
	var out []string
	for _, c := range cards {
		why, ok := staleWhy(c, keep, now)
		if !ok || archive.Known(c.Key, cardLastAt(c)) {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s) leaves the board: %s", c.Ref, c.Column, why))
		if dry || archive == nil {
			continue
		}
		row := watch.Archived{Key: c.Key, Ref: c.Ref, Workspace: c.Workspace, Title: c.Title, URL: c.URL, Kind: c.Kind,
			Column: c.Column, Why: why, LastAt: cardLastAt(c), At: now}
		if c.Cost != nil {
			row.Cost = &watch.Cost{USD: c.Cost.USD, Tokens: c.Cost.Tokens, Runs: c.Cost.Runs, Models: c.Cost.Models, Agents: c.Cost.Agents}
		}
		_ = archive.Put(row)
	}
	return out
}

func cardLastAt(c KanbanCard) time.Time {
	last := c.UpdatedAt
	if c.Fix != nil && c.Fix.StartedAt.After(last) {
		last = c.Fix.StartedAt
	}
	return last
}

func staleWhy(c KanbanCard, keep time.Duration, now time.Time) (string, bool) {
	if c.Key == "" || c.Session != nil || (c.Fix != nil && c.Fix.Running) || c.Column == ColRunning || c.Column == ColBlocked {
		return "", false
	}
	if c.Picked != nil && now.Sub(c.Picked.At) <= watch.PickFresh {
		return "", false
	}
	age := now.Sub(cardLastAt(c))
	days := func() string { return roughAge(age) + " ago" }
	switch c.Column {
	case ColDone:
		return c.Why, true
	case ColReview:
		if c.Pull != nil && pullOver(c.Pull.State) == "" {
			return "", false
		}
		if age > keep {
			return "in review with nothing open since " + days(), true
		}
	default:
		if c.Fix != nil && len(c.Fix.PRs) == 0 && age > keep {
			return "a run answered " + days() + " and nothing happened since", true
		}
		if c.Fix == nil && c.Handoff == nil && age > 2*keep {
			return "nothing happened on it for " + roughAge(age), true
		}
	}
	return "", false
}

var agentKanbanReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Let the cards that are over leave the board",
	Long: `The board keeps what is live. A card that is Done for a day, a run that
answered a week ago with nothing since, a review whose pull request closed,
a ticket nothing touched for two weeks: these leave the board and go to the
history (corgi agent history). A card with a session, a run or an open pull
request on it stays, whatever its age. Anything new about a ticket puts its
card back.

The daemon does this once an hour on its own.

  corgi agent kanban reconcile --dry-run
  corgi agent kanban reconcile --keep 3d
  corgi agent kanban reconcile --back ABC-12     # put a card back by hand`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		dry, _ := cmd.Flags().GetBool(watchFlagDryRun)
		keepFlag, _ := cmd.Flags().GetString("keep")
		back, _ := cmd.Flags().GetString("back")
		archive := watch.LoadArchive(dir)
		if back != "" {
			row, ok := archive.Forget(back)
			if !ok {
				exitWithError("agent_kanban", fmt.Errorf("%s is not in the history", back), 2)
				return
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"back": row.Ref, "row": row})
				return
			}
			fmt.Printf("%s is back on the board\n", row.Ref)
			return
		}
		keep, err := watch.ParseAge(keepFlag)
		if err != nil {
			exitWithError("agent_kanban", err, 2)
			return
		}
		now := time.Now()
		lines := reconcileKanban(gatherKanban(dir, "", now), archive, keep, now, dry)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"dryRun": dry, "keep": keepFlag, "left": lines})
			return
		}
		if len(lines) == 0 {
			fmt.Println("the board is current: nothing to let go")
			return
		}
		for _, l := range lines {
			if dry {
				fmt.Println("would: " + l)
			} else {
				fmt.Println(l)
			}
		}
	},
}

func init() {
	agentKanbanReconcileCmd.Flags().Bool(watchFlagDryRun, false, "Say what would leave and change nothing")
	agentKanbanReconcileCmd.Flags().String("keep", "7d", "How long a quiet card stays, e.g. 7d or 48h")
	agentKanbanReconcileCmd.Flags().String("back", "", "Put this archived card back on the board")
	agentKanbanCmd.AddCommand(agentKanbanReconcileCmd)
}

func reconcileBoard(dir string) func(now time.Time) []string {
	return func(now time.Time) []string {
		return reconcileKanban(gatherKanban(dir, "", now), watch.LoadArchive(dir), reconcileKeep, now, false)
	}
}
