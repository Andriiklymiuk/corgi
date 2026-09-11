package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

var agentWatchUndoCmd = &cobra.Command{
	Use:   "undo [REF]",
	Short: "Put back what the last unattended run did",
	Long: `Closes the pull requests a run opened and moves its ticket back to the column
it was in, so leaving unattended mode on is a decision you can reverse.

  corgi agent watch undo                # the most recent finished run
  corgi agent watch undo ABC-123        # that one
  corgi agent watch undo --dry-run      # say what it would do and stop

The branch is left alone: it holds the work, and deleting it is the one part
that cannot be undone in turn.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentWatchUndo,
}

func runAgentWatchUndo(cmd *cobra.Command, args []string) {
	dir := mustAgentDir()
	dry, _ := cmd.Flags().GetBool("dry-run")
	ref := ""
	if len(args) == 1 {
		ref = strings.TrimSpace(args[0])
	}
	run, ok := lastUndoable(dir, ref)
	if !ok {
		if ref != "" {
			exitWithError("agent_watch_undo", fmt.Errorf("no finished run for %s", ref), 2)
		}
		exitWithError("agent_watch_undo", fmt.Errorf("nothing to undo — no unattended run has finished"), 2)
	}

	back := ""
	if st, ok := watch.LoadStateLog(dir).Get(run.Key); ok {
		back = st.Was
	}
	plan := undoPlan{Ref: firstNonEmptyString(run.Ref, run.Key), Workspace: run.Workspace, PRs: run.PRs, MoveBack: back}
	if dry {
		reportUndo(plan, nil)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var problems []string
	secrets := watch.LoadSecretsFor(dir, run.Workspace)
	for _, pr := range run.PRs {
		if err := watch.ClosePR(ctx, secrets, pr); err != nil {
			problems = append(problems, firstLineOf(err.Error()))
			continue
		}
		plan.Closed = append(plan.Closed, pr)
	}
	if back != "" && run.Ref != "" {
		w, _, err := watchWriter(dir, run.Workspace)
		if err != nil {
			problems = append(problems, err.Error())
		} else if err := w.Move(ctx, run.Ref, back); err != nil {
			problems = append(problems, firstLineOf(err.Error()))
		} else {
			plan.Moved = true
			_ = watch.LoadStateLog(dir).Set(run.Key, back, time.Now())
		}
	}
	// An isolated run's worktrees go too, unless they hold uncommitted work.
	if run.Branch != "" {
		if registry, err := workspace.Load(agentRegistryPath(dir)); err == nil {
			ws, _ := registry.Find(run.Workspace)
			removed, kept, err := releaseFixWorktrees(ws.AbsPath, run.Branch)
			if err != nil {
				problems = append(problems, firstLineOf(err.Error()))
			}
			plan.WorktreesRemoved = removed
			for _, k := range kept {
				problems = append(problems, "kept "+k+": it has uncommitted work")
			}
		}
	}
	// The event goes back in the inbox: undoing a run means it was not done.
	st := watch.LoadState(dir)
	st.Unsee(run.Key)
	_ = st.Unignore(run.Key)
	reportUndo(plan, problems)
}

type undoPlan struct {
	Ref       string   `json:"ref"`
	Workspace string   `json:"workspace"`
	PRs       []string `json:"prs,omitempty"`
	Closed    []string `json:"closed,omitempty"`
	MoveBack  string   `json:"moveBack,omitempty"`
	Moved     bool     `json:"moved,omitempty"`
	// WorktreesRemoved is what an isolated run left that undo cleaned up.
	WorktreesRemoved []string `json:"worktreesRemoved,omitempty"`
}

func reportUndo(p undoPlan, problems []string) {
	if utils.JSONOutput {
		out := map[string]any{"undo": p}
		if len(problems) > 0 {
			out["problems"] = problems
		}
		utils.PrintJSON(out)
		return
	}
	fmt.Printf("%s (%s)\n", p.Ref, p.Workspace)
	if len(p.PRs) == 0 {
		fmt.Println("  it opened nothing")
	}
	for _, pr := range p.PRs {
		if containsString(p.Closed, pr) {
			fmt.Printf("  closed   %s\n", pr)
		} else if len(p.Closed) == 0 && len(problems) == 0 {
			fmt.Printf("  would close %s\n", pr)
		}
	}
	switch {
	case p.Moved:
		fmt.Printf("  moved    back to %s\n", p.MoveBack)
	case p.MoveBack != "":
		fmt.Printf("  would move back to %s\n", p.MoveBack)
	default:
		fmt.Println("  the ticket was never moved, so there is nothing to put back")
	}
	fmt.Println("  the branch is left alone — the work is on it")
	for _, why := range problems {
		fmt.Printf("  problem  %s\n", why)
	}
}

// lastUndoable is the newest finished run, or the newest for one ref.
func lastUndoable(dir, ref string) (watch.FixRecord, bool) {
	for _, r := range watch.LoadFixLog(dir).RecentFixes("", 50) {
		if !r.Done() {
			continue
		}
		if ref != "" && !strings.EqualFold(r.Ref, ref) && !strings.EqualFold(r.Key, ref) {
			continue
		}
		return r, true
	}
	return watch.FixRecord{}, false
}

func init() {
	agentWatchUndoCmd.Flags().Bool("dry-run", false, "Say what it would do and change nothing")
	agentWatchCmd.AddCommand(agentWatchUndoCmd)
}
