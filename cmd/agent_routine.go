package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

// A routine is a run on a clock: the digest at 08:30, the PR babysitter
// every two hours, the dependency triage on Monday. It runs through the
// same runner as an unattended fix — caps, quiet hours, budget, log, cost —
// and its report is one inbox row.
var agentRoutineCmd = &cobra.Command{
	Use:   "routine",
	Short: "Runs on a clock: digest, babysit-pr, deps, release-notes, flaky, doc-drift, or your own",
	Long: `Routines run in a workspace on a schedule, through the daemon, with the same
caps, quiet hours and budget as an unattended fix. Each report is one row in
the inbox with the log behind it.

  corgi agent routine catalog                              # what corgi knows how to run
  corgi agent routine add digest                           # on its default schedule (daily 08:30)
  corgi agent routine add babysit-pr --schedule "every 2h"
  corgi agent routine add nightly-e2e --prompt "run corgi test --e2e and open a ticket per failure" --schedule "daily 03:00"
  corgi agent routine list
  corgi agent routine run digest                           # now, ignoring the clock
  corgi agent routine rm digest

A schedule is "daily HH:MM", "every 6h" (at least 5m) or "weekly Mon HH:MM".
Restart the daemon after add or rm: corgi agent restart.`,
}

var agentRoutineCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "The routines corgi knows how to run",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if utils.JSONOutput {
			utils.PrintJSON(watch.Catalog)
			return
		}
		for _, k := range watch.Catalog {
			fmt.Printf("%-14s %-18s %s\n", k.Name, k.Default, k.What)
		}
	},
}

var agentRoutineAddCmd = &cobra.Command{
	Use:   "add <kind|name>",
	Short: "Schedule a routine in this workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil {
			return err
		}
		if user == nil {
			user = &config.UserConfig{}
		}
		if user.Workspaces == nil {
			user.Workspaces = map[string]config.WorkspaceConfig{}
		}
		name := strings.TrimSpace(args[0])
		prompt, _ := cmd.Flags().GetString("prompt")
		schedule, _ := cmd.Flags().GetString("schedule")
		model, _ := cmd.Flags().GetString("model")
		r := config.Routine{Name: name, Prompt: strings.TrimSpace(prompt), Schedule: strings.TrimSpace(schedule), Model: model}
		if k, ok := watch.CatalogKind(name); ok && r.Prompt == "" {
			r.Kind = k.Name
			if r.Schedule == "" {
				r.Schedule = k.Default
			}
		}
		if r.Kind == "" && r.Prompt == "" {
			return fmt.Errorf("%q is not in the catalog (corgi agent routine catalog); give your own --prompt", name)
		}
		if r.Schedule == "" {
			return fmt.Errorf("--schedule is required for your own routine: \"daily HH:MM\", \"every 6h\" or \"weekly Mon HH:MM\"")
		}
		sched, err := watch.ParseSchedule(r.Schedule)
		if err != nil {
			return err
		}
		entry := user.Workspaces[id]
		kept := entry.Routines[:0]
		for _, x := range entry.Routines {
			if !strings.EqualFold(x.Name, r.Name) {
				kept = append(kept, x)
			}
		}
		entry.Routines = append(kept, r)
		user.Workspaces[id] = entry
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		what := r.Prompt
		if r.Kind != "" {
			k, _ := watch.CatalogKind(r.Kind)
			what = k.What
		}
		utils.Infof("✓ %s in %s: %s — %s\n", r.Name, id, sched.String(), clipTitle(what, 70))
		utils.Info("restart the daemon to pick it up: corgi agent restart")
		return nil
	},
}

var agentRoutineListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every routine, every workspace",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		user, err := config.LoadUser(agentUserConfigPath(dir))
		if err != nil || user == nil {
			fmt.Println("No routines.")
			return
		}
		type row struct {
			Workspace string `json:"workspace"`
			Name      string `json:"name"`
			Kind      string `json:"kind,omitempty"`
			Schedule  string `json:"schedule"`
			Off       bool   `json:"off,omitempty"`
			LastRun   string `json:"lastRun,omitempty"`
		}
		var rows []row
		last := watch.LoadFixLog(dir)
		for ws, entry := range user.Workspaces {
			for _, r := range entry.Routines {
				lr := ""
				for _, f := range last.RecentFixes(ws, 200) {
					if f.Ref == "routine/"+firstNonEmpty(r.Name, r.Kind) {
						lr = roughAge(time.Since(f.StartedAt)) + " ago · " + f.Outcome()
						break
					}
				}
				rows = append(rows, row{Workspace: ws, Name: r.Name, Kind: r.Kind, Schedule: r.Schedule, Off: r.Off, LastRun: lr})
			}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"routines": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Println("No routines. Add one: corgi agent routine add digest")
			return
		}
		for _, r := range rows {
			state := ""
			if r.Off {
				state = " (off)"
			}
			fmt.Printf("%-12s %-14s %-18s %s%s\n", r.Workspace, r.Name, r.Schedule, r.LastRun, state)
		}
	},
}

var agentRoutineRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a routine from this workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil || user == nil {
			return fmt.Errorf("no routines in %s", id)
		}
		entry := user.Workspaces[id]
		kept := entry.Routines[:0]
		found := false
		for _, x := range entry.Routines {
			if strings.EqualFold(x.Name, args[0]) {
				found = true
				continue
			}
			kept = append(kept, x)
		}
		if !found {
			return fmt.Errorf("no routine %q in %s", args[0], id)
		}
		entry.Routines = kept
		user.Workspaces[id] = entry
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("✓ removed %s from %s (after corgi agent restart)\n", args[0], id)
		return nil
	},
}

var agentRoutineRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a routine now, ignoring the clock",
	Long: `Hands the routine to the daemon right away, the way a watch event would
arrive: it runs with the caps, the log and the cost, and reports to the inbox.
The daemon has to be running.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		specs, err := loadWatchSpecs(dir)
		if err != nil {
			return err
		}
		for _, spec := range specs {
			if spec.Workspace != id {
				continue
			}
			for _, r := range spec.Routines {
				if !strings.EqualFold(r.Name, args[0]) && !strings.EqualFold(r.Kind, args[0]) {
					continue
				}
				e, ok := daemon.RoutineEvent(id, r, time.Now())
				if !ok {
					return fmt.Errorf("routine %q has nothing to run", args[0])
				}
				info, _ := daemon.ReadInfo(dir)
				if info == nil || !info.Commands {
					return fmt.Errorf("the daemon is not running: corgi agent serve, or corgi agent up")
				}
				if _, err := command.Write(dir, command.Command{Action: command.ActionWatch, Source: "routine run", WatchEvent: &e}); err != nil {
					return err
				}
				daemon.Nudge(info)
				utils.Infof("✓ %s handed to the daemon — its report lands in the inbox; log: corgi agent watch --json\n", e.Title)
				return nil
			}
		}
		return fmt.Errorf("no routine %q in %s (corgi agent routine list)", args[0], id)
	},
}

func init() {
	for _, c := range []*cobra.Command{agentRoutineAddCmd, agentRoutineRmCmd, agentRoutineRunCmd} {
		c.Flags().String("workspace", "", "the workspace (default: the one you are in)")
	}
	agentRoutineAddCmd.Flags().String("schedule", "", "\"daily HH:MM\", \"every 6h\" or \"weekly Mon HH:MM\" (a catalog kind has a default)")
	agentRoutineAddCmd.Flags().String("prompt", "", "your own routine: what the run should do")
	agentRoutineAddCmd.Flags().String("model", "", "the model to run it on (default: the workspace policy's execute model)")
	agentRoutineCmd.AddCommand(agentRoutineCatalogCmd, agentRoutineAddCmd, agentRoutineListCmd, agentRoutineRmCmd, agentRoutineRunCmd)
	agentCmd.AddCommand(agentRoutineCmd)
}
