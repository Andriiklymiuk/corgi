package cmd

import (
	"fmt"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentSelfCheckCmd = &cobra.Command{
	Use:   "selfcheck [--days N] [--workspace <id>]",
	Short: "What kept going wrong in the unattended runs: checks they skipped and why, machine failures, money on runs that found nothing, items still left for you",
	Long: `Reads the fix log, the same one scorecard reads, and says each thing that
came back more than once, with what corgi does about it or what you can.
The daemon sends it once a week, Monday morning, when there is something to say.

  corgi agent selfcheck
  corgi agent selfcheck --workspace .        # only the workspace you are in
  corgi agent selfcheck --days 30 --json`,
	Run: func(cmd *cobra.Command, _ []string) {
		days, _ := cmd.Flags().GetInt("days")
		if days < 1 {
			days = 7
		}
		dir := mustAgentDir()
		records := watch.RecordsOf(watch.LoadFixLog(dir).Records(), historyWorkspace(cmd, dir))
		c := watch.SelfCheckOf(records, watch.LoadLeftTicks(dir).All(), time.Now(), days)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"headline": c.Headline(), "days": c.Days, "runs": c.Runs, "lines": c.Lines})
			return
		}
		fmt.Println(c.Headline())
		for _, l := range c.Lines {
			fmt.Println("- " + l)
		}
	},
}

func init() {
	agentSelfCheckCmd.Flags().Int("days", 7, "How many days back")
	agentSelfCheckCmd.Flags().String("workspace", "", "Only this workspace (. is the one you are in)")
	agentCmd.AddCommand(agentSelfCheckCmd)
}
