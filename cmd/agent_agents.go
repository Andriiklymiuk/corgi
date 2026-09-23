package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/harness"
	"github.com/spf13/cobra"
)

// A workspace's agents, in the order to try: `agents: [claude, codex]`.
// The first is what corgi agent claude opens and what fixes run through;
// the next takes an unattended run when the first cannot work - not
// installed, login lapsed, window spent. Every run picks again.

// parseAgents reads "claude,codex" (or several flags) into the order.
func parseAgents(raw []string) ([]string, error) {
	var out []string
	for _, chunk := range raw {
		for _, name := range strings.Split(chunk, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if !harness.Known(name) {
				return nil, fmt.Errorf("agents are %s, not %q", strings.Join(harness.Names(), ", "), name)
			}
			dup := false
			for _, have := range out {
				dup = dup || have == name
			}
			if !dup {
				out = append(out, name)
			}
		}
	}
	return out, nil
}

// setAgents writes the order for one workspace, or the defaults.
func setAgents(id string, agents []string, asDefault bool) error {
	dir, err := agentDir()
	if err != nil {
		return err
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	apply := func(w *config.WorkspaceConfig) {
		w.Agents = agents
		w.Kind = ""
		if len(agents) == 1 {
			w.Agents = nil
			if agents[0] != harness.Claude {
				w.Kind = agents[0]
			}
		}
	}
	if asDefault {
		apply(&user.Defaults)
	} else {
		entry, ok := user.Workspaces[id]
		if !ok {
			return fmt.Errorf("no workspace called %q - corgi agent workspaces lists them", id)
		}
		apply(&entry)
		user.Workspaces[id] = entry
	}
	return writeUserConfig(path, user)
}

// agentsWord is the order as a person reads it: "claude → codex".
func agentsWord(order []string) string { return strings.Join(order, " → ") }

// agentsNotice tells what an order means, after init or a change.
func agentsNotice(order []string) {
	if len(order) < 2 {
		if order[0] != harness.Claude {
			utils.Infof("runs through %s\n", order[0])
		}
		return
	}
	utils.Infof("agents %s: %s opens and runs the fixes; %s takes a run when it cannot\n", agentsWord(order), order[0], strings.Join(order[1:], ", then "))
	utils.Info("  (not installed, login lapsed, window spent - checked at every run, nothing is remembered)")
	for _, name := range order {
		if !harness.For(name, "").Installed() {
			utils.Infof("  %s is not installed here - it is skipped until it is\n", name)
		}
	}
}

var agentWorkspacesAgentsCmd = &cobra.Command{
	Use:   "agents <id> <claude,codex>",
	Short: "The agents a workspace tries, in order: the first runs, the next takes over when it cannot",
	Long: `Sets the agents of one workspace, first choice first.

  corgi agent workspaces agents api claude,codex   # claude; codex when claude is out
  corgi agent workspaces agents api codex          # codex alone
  corgi agent workspaces agents --default claude,codex

Every unattended run picks the first agent that can work right now - installed,
logged in, window not spent - so the first one gets the next run back the
moment it can. The pick rings your phone once a day when it moves.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		asDefault, _ := cmd.Flags().GetBool("default")
		id, raw := "", args[len(args)-1]
		if !asDefault {
			if len(args) != 2 {
				return fmt.Errorf("corgi agent workspaces agents <id> <claude,codex>, or --default <claude,codex>")
			}
			id = args[0]
		}
		order, err := parseAgents([]string{raw})
		if err != nil {
			return err
		}
		if len(order) == 0 {
			return fmt.Errorf("name at least one agent: %s", strings.Join(harness.Names(), ", "))
		}
		if err := setAgents(id, order, asDefault); err != nil {
			return err
		}
		if asDefault {
			utils.Infof("every workspace without agents of its own: %s\n", agentsWord(order))
		} else {
			utils.Infof("%s: %s\n", id, agentsWord(order))
		}
		agentsNotice(order)
		utils.Info("restart the daemon to pick it up: corgi agent restart")
		return nil
	},
}

func init() {
	agentWorkspacesAgentsCmd.Flags().Bool("default", false, "Set the order every workspace without one of its own uses")
	agentWorkspacesCmd.AddCommand(agentWorkspacesAgentsCmd)
}
