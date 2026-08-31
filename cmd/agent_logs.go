package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
)

var agentLogsCmd = &cobra.Command{
	Use:     "logs <workspace>",
	Aliases: []string{"events", "timeline"},
	Short:   "Show a workspace's session timeline (starts, exits, why)",
	Args:    cobra.MinimumNArgs(1),
	Run:     runAgentLogs,
}

func runAgentLogs(cmd *cobra.Command, args []string) {
	registry, _ := mustLoadRegistry()
	res := workspace.Resolve(registry, strings.Join(args, " "))
	if !res.Resolved() {
		if utils.JSONOutput {
			utils.PrintJSON(res)
		} else {
			fmt.Println(res.Reason)
			for _, c := range res.Candidates {
				fmt.Printf("  %-20s %s\n", c.Workspace.ID, c.Workspace.AbsPath)
			}
		}
		exitProcess(2)
	}

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	timeline := events.NewLog(dir).Read(res.Workspace.ID, limit)

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"workspaceId": res.Workspace.ID, "events": timeline})
		return
	}
	if len(timeline) == 0 {
		utils.Infof("no events recorded for %s yet — the timeline fills as the daemon runs sessions\n", res.Workspace.ID)
		return
	}
	for _, e := range timeline {
		fmt.Printf("%s  %s\n", e.At.Local().Format("Jan 02 15:04:05"), describeEvent(e))
	}
}

func describeEvent(e events.Event) string {
	switch e.Kind {
	case "started":
		return fmt.Sprintf("started (pid %d)", e.PID)
	case "session":
		return "session " + e.URL
	case "disabled":
		return "disabled — " + e.Reason
	case "exited":
		s := "exited"
		if e.Cause != "" {
			s += " · " + e.Cause
		}
		if e.Reason != "" {
			s += " — " + e.Reason
		}
		return s
	}
	return e.Kind
}

func init() {
	agentLogsCmd.Flags().Int("limit", 30, "How many events to show, newest first (0 = all kept)")
	agentCmd.AddCommand(agentLogsCmd)
}
