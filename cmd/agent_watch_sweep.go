package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var agentWatchSweepCmd = &cobra.Command{
	Use:   "sweep",
	Short: "Hand every open ticket already assigned to you to the daemon, as if it had just arrived",
	Long: `The watch only sees what changes after it starts. Work that was already
yours — assigned last week, sitting in a column the watch takes — never
arrives on its own. A sweep reads the tracker once for every open ticket
assigned to you and hands each to the daemon as a new issue. The rules still
decide (column, labels, action), and a ticket the daemon already ran on, or
one you told it to ignore, is left alone.

  corgi agent watch sweep --dry-run                    what it would hand over
  corgi agent watch sweep --states "Ready for dev"     only that column
  corgi agent watch sweep                              hand them over

Without --states the workspace's own column list applies, and that list often
holds In Progress and In Review for the sake of comments — columns where the
work is already yours by hand. Name the column that means "not started".`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		specs, err := loadWatchSpecs(dir)
		if err != nil {
			return err
		}
		only, _ := cmd.Flags().GetString("workspace")
		dryRun, _ := cmd.Flags().GetBool(watchFlagDryRun)
		states, _ := cmd.Flags().GetStringSlice("states")
		state := watch.LoadState(dir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var found []watch.Event
		for _, spec := range specs {
			if only != "" && spec.Workspace != only {
				continue
			}
			if len(states) > 0 {
				spec.Rules.States = states
			}
			events, err := sweepWorkspace(ctx, spec, state)
			if err != nil {
				utils.Infof("%s: %v\n", spec.Workspace, err)
				continue
			}
			found = append(found, events...)
		}
		if utils.JSONOutput {
			utils.PrintJSON(found)
			return nil
		}
		if len(found) == 0 {
			utils.Info("nothing waiting that the daemon does not know")
			return nil
		}
		for _, e := range found {
			utils.Infof("%-8s %-10s %-14s %s\n", e.Workspace, e.Ref, e.State, firstLineOf(e.Title))
		}
		if dryRun {
			return nil
		}
		info, _ := daemon.ReadInfo(dir)
		if info == nil || !info.Commands {
			return fmt.Errorf("no daemon to hand them to — corgi agent start")
		}
		queued := 0
		for i := range found {
			e := found[i]
			if _, err := command.Write(dir, command.Command{Action: command.ActionWatch, Source: "watch sweep", WatchEvent: &e}); err == nil {
				queued++
			}
		}
		daemon.Nudge(info)
		utils.Infof("%d ticket(s) handed to the daemon\n", queued)
		return nil
	},
}

type miner interface {
	Mine(ctx context.Context) ([]watch.Event, error)
}

// The open tickets of mine the rules take and the daemon has not run on,
// ignored, or blocked.
func sweepWorkspace(ctx context.Context, spec daemon.WatchSpec, state *watch.State) ([]watch.Event, error) {
	var out []watch.Event
	for _, src := range spec.Sources {
		m, ok := src.(miner)
		if !ok {
			continue
		}
		events, err := m.Mine(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			e.Workspace = spec.Workspace
			if why := spec.Rules.Why(e); why != "" {
				continue
			}
			if skip := sweepSkips(state, spec.Workspace, e); skip != "" {
				continue
			}
			out = append(out, e)
		}
	}
	return out, nil
}

func sweepSkips(state *watch.State, workspace string, e watch.Event) string {
	if state.IsSeen(e.Key) {
		return "seen"
	}
	if state.IsIgnored(e.Key) {
		return "ignored"
	}
	if _, blocked := state.Fixes.Blocked(workspace, e.Ref); blocked {
		return "blocked"
	}
	for _, r := range state.Fixes.RecentFixes(workspace, 500) {
		if strings.EqualFold(r.Ref, e.Ref) {
			return "ran already"
		}
	}
	return ""
}
