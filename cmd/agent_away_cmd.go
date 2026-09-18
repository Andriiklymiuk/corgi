package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

type awayStep struct {
	What  string
	Apply func() error
}

func awayPlan(user *config.UserConfig, dirs map[string]string) []awayStep {
	var steps []awayStep
	for _, id := range sortedWorkspaceIDs(user) {
		id := id
		wc := user.Workspaces[id]
		if wc.Watch == nil || !wc.Watch.Enabled {
			continue
		}
		if !hasRoutine(wc.Routines, "digest") {
			steps = append(steps, awayStep{What: "digest routine in " + id, Apply: func() error {
				k, _ := watch.CatalogKind("digest")
				entry := user.Workspaces[id]
				entry.Routines = append(entry.Routines, config.Routine{Name: "digest", Kind: k.Name, Schedule: k.Default})
				user.Workspaces[id] = entry
				return nil
			}})
		}
		if wc.Watch.Action != "fix" {
			continue
		}
		if !wc.Watch.Isolate || wc.Watch.PruneAfter == "" {
			steps = append(steps, awayStep{What: "worktrees + prune in " + id, Apply: func() error {
				entry := user.Workspaces[id]
				entry.Watch.Isolate = true
				if entry.Watch.PruneAfter == "" {
					entry.Watch.PruneAfter = "7d"
				}
				user.Workspaces[id] = entry
				return nil
			}})
		}
		if dir := dirs[id]; dir != "" {
			steps = append(steps, awayStep{What: "harden " + id, Apply: func() error {
				_, err := hardenSettings(claudeLocalSettingsPath(dir), corgiCommandPath(), false)
				return err
			}})
		}
	}
	return steps
}

var agentAwayCmd = &cobra.Command{
	Use:   "away",
	Short: "Set this machine up to work alone for weeks, and say what only you can do",
	Long: `Runs the away doctor, applies every corgi-side fix (a digest routine per
watched workspace; worktrees, pruning and harden where fixes run unattended)
and prints the sudo lines corgi cannot run for you. Action, quiet hours and
caps are yours: it changes none of them.

  corgi agent away                       # apply, then corgi agent restart
  corgi agent away --dry-run             # only say
  corgi agent away --pulse <url>         # save the dead-man URL too`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir, err := agentDir()
		if err != nil {
			return err
		}
		dry, _ := cmd.Flags().GetBool("dry-run")
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil {
			return err
		}
		if user == nil {
			user = &config.UserConfig{}
		}
		pulse, _ := cmd.Flags().GetString("pulse")
		if pulse != "" && !dry {
			user.PulseUrl = pulse
		}
		registry, _ := mustLoadRegistry()
		dirs := map[string]string{}
		for _, w := range registry.Workspaces {
			dirs[w.ID] = w.AbsPath
		}
		steps := awayPlan(user, dirs)
		for _, s := range steps {
			if dry {
				fmt.Printf("would: %s\n", s.What)
				continue
			}
			if err := s.Apply(); err != nil {
				return fmt.Errorf("%s: %w", s.What, err)
			}
			fmt.Printf("✓ %s\n", s.What)
		}
		if !dry && (len(steps) > 0 || pulse != "") {
			if err := writeUserConfig(path, user); err != nil {
				return err
			}
		}
		planned := map[string]bool{}
		for _, s := range steps {
			planned[s.What] = true
		}
		var todo []agentCheck
		for _, c := range awayChecks(dir) {
			if c.OK || c.Fix == "" || coveredByPlan(c.Name, planned) {
				continue
			}
			todo = append(todo, c)
		}
		if len(todo) == 0 {
			fmt.Println("✓ nothing left that only you can do")
		} else {
			fmt.Println("\nOnly you can do these:")
			for _, c := range todo {
				fmt.Printf("  %-24s %s\n  %-24s → %s\n", c.Name, c.Detail, "", c.Fix)
			}
		}
		if !dry && (len(steps) > 0 || pulse != "") {
			fmt.Println("\ncorgi agent restart so the daemon picks it up")
		}
		return nil
	},
}

// In a dry run the digest and isolation rows are what the plan would fix,
// not something only the person can do.
func coveredByPlan(check string, planned map[string]bool) bool {
	for _, prefix := range []string{"digest · ", "isolate · "} {
		if ws, ok := strings.CutPrefix(check, prefix); ok {
			return planned["digest routine in "+ws] || planned["worktrees + prune in "+ws]
		}
	}
	return false
}

func init() {
	agentAwayCmd.Flags().Bool("dry-run", false, "Say what would change, change nothing")
	agentAwayCmd.Flags().String("pulse", "", "Dead-man URL to save (same as corgi agent pulse <url>)")
	agentCmd.AddCommand(agentAwayCmd)
}
