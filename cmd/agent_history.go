package cmd

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

type HistoryEntry struct {
	At        time.Time    `json:"at"`
	Kind      string       `json:"kind"`
	Key       string       `json:"key,omitempty"`
	Workspace string       `json:"workspace,omitempty"`
	Ref       string       `json:"ref"`
	Title     string       `json:"title,omitempty"`
	URL       string       `json:"url,omitempty"`
	Why       string       `json:"why"`
	Cost      *CardCost    `json:"cost,omitempty"`
	PRs       []string     `json:"prs,omitempty"`
	StartedAt time.Time    `json:"startedAt,omitzero"`
	Took      string       `json:"took,omitempty"`
	Feature   *CardFeature `json:"feature,omitempty"`
}

const (
	HistRun      = "run"
	HistMerged   = "merged"
	HistDone     = "done"
	HistLeft     = "left the board"
	HistCanceled = "canceled"
)

type historyInputs struct {
	runs    []watch.FixRecord
	archive []watch.Archived
	pulls   map[string]watch.PullStatus
	tasks   []watch.Task
	plans   []watch.Plan
	since   time.Time
}

func buildHistory(in historyInputs) []HistoryEntry {
	var out []HistoryEntry
	planOf := map[string]*CardFeature{}
	for _, p := range in.plans {
		for _, id := range p.TaskIDs() {
			planOf[p.Workspace+"/"+fmt.Sprintf("TASK-%d", id)] = &CardFeature{Ref: p.Ref(), Title: firstLineOf(p.Goal)}
		}
	}
	for _, r := range in.runs {
		if !r.Done() || r.Ref == "" || strings.HasPrefix(r.Ref, "routine/") || r.FinishedAt.Before(in.since) || strings.HasPrefix(r.Error, "not started") {
			continue
		}
		var cost watch.Cost
		cost.Runs, cost.USD, cost.Tokens = 1, r.CostUSD, r.Tokens
		cost.AddWorker(r.Harness, r.Model)
		e := HistoryEntry{At: r.FinishedAt, Kind: HistRun, Key: r.Key, Workspace: r.Workspace, Ref: r.Ref, Title: firstLineOf(r.Title), URL: r.URL,
			Why: firstLineOf(r.Outcome()), PRs: r.PRs, StartedAt: r.StartedAt, Took: roughAge(r.FinishedAt.Sub(r.StartedAt)),
			Cost: &CardCost{USD: cost.USD, Tokens: cost.Tokens, Runs: 1, Models: cost.Models, Agents: cost.Agents}, Feature: planOf[r.Workspace+"/"+r.Ref]}
		out = append(out, e)
	}
	for _, a := range in.archive {
		if a.At.Before(in.since) {
			continue
		}
		kind := HistLeft
		if a.Column == ColDone {
			kind = HistDone
		}
		e := HistoryEntry{At: a.At, Kind: kind, Key: a.Key, Workspace: a.Workspace, Ref: a.Ref, Title: a.Title, URL: a.URL, Why: a.Why, Feature: planOf[a.Workspace+"/"+a.Ref]}
		if a.Cost != nil {
			e.Cost = &CardCost{USD: a.Cost.USD, Tokens: a.Cost.Tokens, Runs: a.Cost.Runs, Models: a.Cost.Models, Agents: a.Cost.Agents}
		}
		out = append(out, e)
	}
	for ref, p := range in.pulls {
		if pullOver(p.State) != "merged" || p.At.Before(in.since) {
			continue
		}
		out = append(out, HistoryEntry{At: p.At, Kind: HistMerged, Workspace: p.Workspace, Ref: ref, URL: p.Link, Why: "merged"})
	}
	for _, t := range in.tasks {
		if watch.FinishedState(t.State) == "" || t.UpdatedAt.Before(in.since) {
			continue
		}
		kind := HistDone
		if t.State == "Canceled" {
			kind = HistCanceled
		}
		why := strings.ToLower(t.State)
		if n := len(t.Comments); n > 0 && t.Comments[n-1].By == watch.TaskAgent {
			why = firstLineOf(t.Comments[n-1].Text)
		}
		out = append(out, HistoryEntry{At: t.UpdatedAt, Kind: kind, Key: t.Key(), Workspace: t.Workspace, Ref: t.Ref(), Title: t.Title, Why: why, Feature: planOf[t.Workspace+"/"+t.Ref()]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func gatherHistory(dir, onlyWorkspace string, since time.Time) []HistoryEntry {
	in := historyInputs{
		runs:    watch.LoadFixLog(dir).Records(),
		archive: watch.LoadArchive(dir).Since(since),
		pulls:   watch.LoadPullLog(dir).All(),
		tasks:   watch.LoadTasks(dir).Tasks,
		plans:   watch.LoadPlans(dir).All(),
		since:   since,
	}
	rows := buildHistory(in)
	if onlyWorkspace == "" {
		return rows
	}
	kept := rows[:0]
	for _, r := range rows {
		if r.Workspace == onlyWorkspace {
			kept = append(kept, r)
		}
	}
	return kept
}

var agentHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "What was worked on, day by day: runs, merges, finished tasks, cards that left",
	Long: `The board shows what is live; this is what already happened. Every run
with what it said, cost and model; every pull request that merged; every
task finished; every card the board let go and why. Newest first, grouped
by day.

  corgi agent history
  corgi agent history --days 30 --workspace api
  corgi agent history --json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		days, _ := cmd.Flags().GetInt("days")
		ws := historyWorkspace(cmd, dir)
		if days <= 0 {
			days = 14
		}
		since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		rows := gatherHistory(dir, ws, since)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"since": since, "entries": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Printf("nothing in the last %d days\n", days)
			return
		}
		day := ""
		for _, r := range rows {
			if d := r.At.Local().Format("Mon 2 Jan"); d != day {
				day = d
				fmt.Println(day)
			}
			fmt.Println("  " + historyLine(r))
		}
	},
}

func historyWorkspace(cmd *cobra.Command, dir string) string {
	ws, _ := cmd.Flags().GetString("workspace")
	if strings.TrimSpace(ws) != "." {
		return ws
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return workspaceIDForDir(dir, cwd)
}

func lastRunKey(records []watch.FixRecord, arg, workspace string) (watch.FixRecord, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if workspace != "" && r.Workspace != workspace {
			continue
		}
		if r.Key == arg || strings.EqualFold(r.Ref, arg) {
			return r, true
		}
	}
	return watch.FixRecord{}, false
}

var agentHistoryLogCmd = &cobra.Command{
	Use:   "log <ref|key>",
	Short: "What the last run on a ticket printed: its prompt, what it did, its outcome and cost",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		tail, _ := cmd.Flags().GetInt("tail")
		ws := historyWorkspace(cmd, dir)
		rec, ok := lastRunKey(watch.LoadFixLog(dir).Records(), args[0], ws)
		if !ok {
			exitWithError("agent_history", fmt.Errorf("no run on %s", args[0]), 2)
			return
		}
		text := daemon.RunLog(dir, rec.Key, tail)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"key": rec.Key, "ref": rec.Ref, "workspace": rec.Workspace, "said": rec.Said, "error": rec.Error, "log": text})
			return
		}
		if text == "" {
			fmt.Printf("%s ran, but its log is gone\n", rec.Ref)
			return
		}
		fmt.Print(text)
	},
}

func historyLine(r HistoryEntry) string {
	line := fmt.Sprintf("%s  %-14s %s", r.At.Local().Format("15:04"), r.Ref, r.Kind)
	if r.Why != "" && r.Why != r.Kind {
		line += ": " + clipTitle(r.Why, 70)
	}
	if r.Cost != nil && r.Cost.Tokens > 0 {
		line += " · " + costLine(*r.Cost)
	}
	if r.Cost != nil && len(r.Cost.Models) > 0 {
		line += " · " + strings.Join(r.Cost.Models, ", ")
	}
	if r.Took != "" {
		line += " · " + r.Took
	}
	return line
}

func launchHistoryHandler(w http.ResponseWriter, r *http.Request) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	days := 14
	if n, err := fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days); n != 1 || err != nil || days <= 0 || days > 365 {
		days = 14
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	writeLaunchJSON(w, map[string]any{"since": since, "entries": gatherHistory(dir, strings.TrimSpace(r.URL.Query().Get("workspace")), since)})
}

func init() {
	agentHistoryCmd.Flags().Int("days", 14, "How far back")
	agentHistoryCmd.Flags().String("workspace", "", "Only this workspace (. is the one you are in)")
	agentHistoryLogCmd.Flags().Int("tail", 300, "Lines from the end of the log")
	agentHistoryLogCmd.Flags().String("workspace", "", "Only this workspace (. is the one you are in)")
	agentHistoryCmd.AddCommand(agentHistoryLogCmd)
	agentCmd.AddCommand(agentHistoryCmd)
}
