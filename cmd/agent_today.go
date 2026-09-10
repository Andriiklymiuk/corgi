package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
)

var agentTodayCmd = &cobra.Command{
	Use:   "today",
	Short: "What has been done today — yours and the watch's",
	Long: `Answers "what have I done today?" in one place: the commits that landed, what
Claude was asked, and — the part nothing else records — what ` + "`corgi agent watch --auto`" + `
did on its own while nobody was looking, with the pull requests it opened.

The window is today since midnight, not a rolling 24 hours, so the answer does
not drift into yesterday evening.

  corgi agent today                # since midnight
  corgi agent today --since 72h    # a longer stretch
  corgi agent today --write        # a few sentences from claude -p
  corgi agent today --json         # the same, machine readable`,
	Run: runAgentToday,
}

// todaySince is midnight local, unless --since asked for a rolling window.
func todaySince(cmd *cobra.Command, now time.Time) (time.Time, error) {
	raw, _ := cmd.Flags().GetString("since")
	if strings.TrimSpace(raw) == "" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	dur, err := time.ParseDuration(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since takes a duration like 8h, not %q", raw)
	}
	return now.Add(-dur), nil
}

type todayTotals struct {
	Workspaces int `json:"workspaces"`
	Commits    int `json:"commits"`
	Prompts    int `json:"prompts"`
	Fixes      int `json:"fixes"`
	Running    int `json:"running"`
	PRs        int `json:"pullRequests"`
	Arrived    int `json:"arrived"`
	Deferred   int `json:"deferred"`
}

func countToday(entries []standupEntry) todayTotals {
	t := todayTotals{Workspaces: len(entries)}
	for _, e := range entries {
		t.Commits += len(e.Commits)
		t.Prompts += len(e.Prompts)
		t.Fixes += len(e.Fixes)
		t.Arrived += len(e.Arrived)
		t.Deferred += len(e.Deferred)
		for _, f := range e.Fixes {
			if f.Running {
				t.Running++
			}
			t.PRs += len(f.PRs)
		}
	}
	return t
}

// headline is the one line worth reading if you read nothing else.
func (t todayTotals) headline() string {
	var parts []string
	if t.Commits > 0 {
		parts = append(parts, plural(t.Commits, "commit", "commits"))
	}
	if t.PRs > 0 {
		parts = append(parts, plural(t.PRs, "pull request", "pull requests")+" from the watch")
	}
	if t.Fixes > 0 {
		parts = append(parts, plural(t.Fixes, "unattended run", "unattended runs"))
	}
	if t.Running > 0 {
		parts = append(parts, fmt.Sprintf("%d still running", t.Running))
	}
	if t.Arrived > 0 {
		parts = append(parts, plural(t.Arrived, "thing arrived", "things arrived"))
	}
	if t.Deferred > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", t.Deferred))
	}
	if len(parts) == 0 {
		return "nothing yet"
	}
	return strings.Join(parts, " · ")
}

func runAgentToday(cmd *cobra.Command, _ []string) {
	write, _ := cmd.Flags().GetBool("write")
	since, err := todaySince(cmd, time.Now())
	if err != nil {
		exitWithError("agent_today", err, 2)
	}
	entries := collectStandup(since)
	totals := countToday(entries)
	if utils.JSONOutput && !write {
		utils.PrintJSON(map[string]any{"since": since, "totals": totals, "workspaces": entries})
		return
	}
	text := formatToday(entries, totals, since)
	if !write {
		fmt.Print(text)
		return
	}
	summary, err := summarizeWithClaude(context.Background(), text)
	if err != nil {
		exitWithError("agent_today", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"since": since, "totals": totals, "summary": summary})
		return
	}
	fmt.Println(summary)
}

func formatToday(entries []standupEntry, totals todayTotals, since time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s since %s\n", since.Local().Format("Mon 2 Jan"), since.Local().Format("15:04"))
	fmt.Fprintf(&b, "%s\n", totals.headline())
	writeStandupBody(&b, entries)
	return b.String()
}

func init() {
	agentTodayCmd.Flags().String("since", "", "A rolling window like 8h instead of since midnight")
	agentTodayCmd.Flags().Bool("write", false, "Have claude -p turn the list into a few sentences")
	agentCmd.AddCommand(agentTodayCmd)
}

// todayForMCP is the same day report the CLI prints, for a chat asking "what
// have you done today?". A rolling window is a duration; empty means midnight.
func todayForMCP(since string, now time.Time) (map[string]any, error) {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if raw := strings.TrimSpace(since); raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("since takes a duration like 8h, not %q", raw)
		}
		from = now.Add(-dur)
	}
	entries := collectStandup(from)
	totals := countToday(entries)
	return map[string]any{
		"since":      from,
		"totals":     totals,
		"headline":   totals.headline(),
		"workspaces": entries,
	}, nil
}
