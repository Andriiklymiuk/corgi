package cmd

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

// Cost, three ways: by repo (workspace), by day, by bot. Two books make
// it: the daemon's day ledger, which books every live session's tokens to
// its workspace as the sweep counts them, and the fix log, where every
// unattended run wrote what claude said it cost. Tokens are what both
// have; dollars only the runs — a session's are on the invoice.

// CostRow is one line of the table.
type CostRow struct {
	Key     string  `json:"key"`
	Tokens  int64   `json:"tokens"`
	USD     float64 `json:"usd,omitempty"`
	Runs    int     `json:"runs,omitempty"`
	Prompts int     `json:"prompts,omitempty"`
	// DayCap is the workspace's day budget, on the repo table.
	DayCap int64 `json:"dayCap,omitempty"`
}

// costBy builds the table for the last n days.
func costBy(dir, by string, n int, now time.Time) ([]CostRow, error) {
	if n < 1 {
		n = 14
	}
	dates := make([]string, 0, n)
	for i := n - 1; i >= 0; i-- {
		dates = append(dates, usage.Today(now.AddDate(0, 0, -i)))
	}
	days := usage.ReadLedgerDays(dir, dates)
	since := now.AddDate(0, 0, -n)
	fixes := watch.LoadFixLog(dir).RecentFixes("", 2000)
	rows := map[string]*CostRow{}
	row := func(k string) *CostRow {
		if r, ok := rows[k]; ok {
			return r
		}
		r := &CostRow{Key: k}
		rows[k] = r
		return r
	}
	switch by {
	case "repo":
		for _, d := range days {
			for ws, t := range d.Tokens {
				row(ws).Tokens += t
			}
		}
		for _, f := range fixes {
			if f.StartedAt.Before(since) {
				continue
			}
			r := row(f.Workspace)
			r.Tokens += f.Tokens
			r.USD += f.CostUSD
			r.Runs++
		}
		if user, err := config.LoadUser(agentUserConfigPath(dir)); err == nil && user != nil {
			for id, wc := range user.Workspaces {
				if wc.Watch != nil && wc.Watch.DayCap > 0 {
					row(id).DayCap = wc.Watch.DayCap
				}
			}
		}
	case "day":
		for _, date := range dates {
			r := row(date)
			if d, ok := days[date]; ok {
				r.Tokens += d.TokensTotal
				r.Prompts = d.Messages
			}
		}
		for _, f := range fixes {
			if f.StartedAt.Before(since) {
				continue
			}
			r := row(usage.Today(f.StartedAt))
			r.Tokens += f.Tokens
			r.USD += f.CostUSD
			r.Runs++
		}
	case "bot":
		for _, f := range fixes {
			if f.StartedAt.Before(since) || f.Bot == "" {
				continue
			}
			r := row(f.Bot)
			r.Tokens += f.Tokens
			r.USD += f.CostUSD
			r.Runs++
		}
	default:
		return nil, fmt.Errorf("--by repo, day or bot")
	}
	out := make([]CostRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	if by == "day" {
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	} else {
		sort.Slice(out, func(i, j int) bool {
			if out[i].Tokens != out[j].Tokens {
				return out[i].Tokens > out[j].Tokens
			}
			return out[i].Key < out[j].Key
		})
	}
	return out, nil
}

var agentCostCmd = &cobra.Command{
	Use:   "cost --by repo|day|bot [--days N]",
	Short: "What the agents cost: tokens by repo, by day, or by bot",
	Long: `Two books: the daemon's day ledger (every live session's tokens, booked to
its workspace as the sweep counts them) and the fix log (what each unattended
run said it cost). Tokens are in both; dollars only where claude gave a
receipt — a session's are on the invoice.

  corgi agent cost --by repo
  corgi agent cost --by day --days 7
  corgi agent cost --by bot
  corgi agent cap --repo api 20M        # a day budget; the daemon rings once past it`,
	Run: func(cmd *cobra.Command, _ []string) {
		by, _ := cmd.Flags().GetString("by")
		n, _ := cmd.Flags().GetInt("days")
		rows, err := costBy(mustAgentDir(), by, n, time.Now())
		if err != nil {
			exitWithError(utils.ErrUsage, err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"by": by, "days": n, "rows": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Printf("nothing counted in the last %d days\n", n)
			return
		}
		fmt.Printf("%-24s %10s %9s %5s\n", strings.ToUpper(by), "TOKENS", "USD", "RUNS")
		for _, r := range rows {
			line := fmt.Sprintf("%-24s %10s %9s %5s", r.Key, sessions.Tokens(r.Tokens), usdWord(r.USD), zeroBlank(r.Runs))
			if r.DayCap > 0 {
				line += "   cap " + sessions.Tokens(r.DayCap) + "/day"
			}
			fmt.Println(line)
		}
	},
}

func usdWord(v float64) string {
	if v == 0 {
		return ""
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

func zeroBlank(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// launchCostHandler is GET /launch/cost?by=repo|day|bot&days=N, the same
// table for the phone.
func launchCostHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET /launch/cost?by=repo|day|bot&days=N")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "repo"
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if n < 1 || n > 60 {
		n = 14
	}
	rows, err := costBy(dir, by, n, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"by": by, "days": n, "rows": rows})
}

func init() {
	agentCostCmd.Flags().String("by", "repo", "repo, day or bot")
	agentCostCmd.Flags().Int("days", 14, "How many days back")
	agentCmd.AddCommand(agentCostCmd)
}
