package cmd

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

func scorecardFor(dir, workspace string, days int, now time.Time) ([]watch.ScoreRow, []watch.ScoreRow) {
	rows := watch.Scorecard(watch.LoadFixLog(dir).Records(), workspace, now.AddDate(0, 0, -days))
	return rows, watch.IdleKinds(rows)
}

func idleHint(r watch.ScoreRow) string {
	return fmt.Sprintf("%s: %d of %d %s runs found nothing to do - leave it out of --auto-for?", r.Workspace, r.Idle, r.Runs-r.Running, r.Kind)
}

var agentScorecardCmd = &cobra.Command{
	Use:   "scorecard [--days N] [--repo <workspace>]",
	Short: "What the unattended runs came to: pushed, green, failed or idle, by workspace and kind",
	Long: `Reads the fix log. A run is green when the checks of the pull request it
worked on passed after it, failed when
it ended in an error. Each run also says its outcome in one line - pushed,
replied, nothing or blocked; a kind that mostly says nothing is worth turning off.
Runs from before the outcome line count only in RUNS.

  corgi agent scorecard
  corgi agent scorecard --days 30 --repo api --json`,
	Run: func(cmd *cobra.Command, _ []string) {
		days, _ := cmd.Flags().GetInt("days")
		if days < 1 {
			days = 7
		}
		repo, _ := cmd.Flags().GetString("repo")
		rows, idle := scorecardFor(mustAgentDir(), repo, days, time.Now())
		if utils.JSONOutput {
			hints := make([]string, 0, len(idle))
			for _, r := range idle {
				hints = append(hints, idleHint(r))
			}
			utils.PrintJSON(map[string]any{"days": days, "rows": rows, "hints": hints})
			return
		}
		if len(rows) == 0 {
			fmt.Printf("no unattended runs in the last %d days\n", days)
			return
		}
		fmt.Printf("%-16s %-17s %4s %6s %5s %7s %7s %7s %6s %8s %8s\n", "WORKSPACE", "KIND", "RUNS", "PUSHED", "GREEN", "REPLIED", "NOTHING", "BLOCKED", "FAILED", "TOKENS", "USD")
		for _, r := range rows {
			fmt.Printf("%-16s %-17s %4d %6s %5s %7s %7s %7s %6s %8s %8s\n", r.Workspace, r.Kind, r.Runs,
				zeroBlank(r.Pushed), zeroBlank(r.Green), zeroBlank(r.Replied), zeroBlank(r.Idle), zeroBlank(r.Blocked), zeroBlank(r.Failed),
				sessions.Tokens(r.Tokens), usdWord(r.USD))
		}
		for _, r := range idle {
			fmt.Println()
			fmt.Println(idleHint(r))
		}
	},
}

func launchScorecardHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET /launch/scorecard?days=N&repo=<workspace>")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 60 {
		days = 7
	}
	rows, idle := scorecardFor(dir, r.URL.Query().Get("repo"), days, time.Now())
	hints := make([]string, 0, len(idle))
	for _, row := range idle {
		hints = append(hints, idleHint(row))
	}
	writeLaunchJSON(w, map[string]any{"days": days, "rows": rows, "hints": hints})
}

func init() {
	agentScorecardCmd.Flags().Int("days", 7, "How many days back")
	agentScorecardCmd.Flags().String("repo", "", "Only this workspace")
	agentCmd.AddCommand(agentScorecardCmd)
}
