package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var agentWatchReplayCmd = &cobra.Command{
	Use:   "replay",
	Short: "The week unattended mode would have had",
	Long: `Runs the events already recorded back through the rules and prints the week
that would have happened: what would have been worked on, what would only have
been reported, and what a cap or quiet hours would have held back.

  corgi agent watch replay                    # the last week
  corgi agent watch replay --since 72h
  corgi agent watch replay --auto-for tickets # try a setting without saving it

Nobody turns unattended mode on for one event. ` + "`corgi agent watch test`" + ` answers
for one; this answers for a week, which is the question actually being asked.`,
	Run: runAgentWatchReplay,
}

type replayRow struct {
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Would string `json:"would"`
	Why   string `json:"why,omitempty"`
}

func runAgentWatchReplay(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	sinceFlag, _ := cmd.Flags().GetString("since")
	dur, err := time.ParseDuration(sinceFlag)
	if err != nil {
		exitWithError("agent_watch_replay", fmt.Errorf("--since takes a duration like 168h, not %q", sinceFlag), 2)
	}
	since := time.Now().Add(-dur)

	specs, err := loadWatchSpecs(dir)
	if err != nil {
		exitWithError("agent_watch_replay", err, 1)
	}
	byWorkspace := map[string]daemon.WatchSpec{}
	for _, s := range specs {
		byWorkspace[s.Workspace] = s
	}
	// A setting tried on the spot, without saving it anywhere.
	if raw, _ := cmd.Flags().GetString("auto-for"); cmd.Flags().Changed("auto-for") {
		kinds, err := parseAutoFor(raw)
		if err != nil {
			exitWithError("agent_watch_replay", err, 2)
		}
		for id, s := range byWorkspace {
			s.FixKinds = kinds
			s.Action = "fix"
			byWorkspace[id] = s
		}
	}

	rows := map[string][]replayRow{}
	for _, e := range watch.EventsSince(dir, since) {
		spec, ok := byWorkspace[e.Workspace]
		if !ok {
			continue
		}
		row := replayRow{Ref: firstNonEmptyString(e.Ref, e.Key), Kind: string(e.Kind)}
		switch why := spec.Rules.Why(e); {
		case why != "":
			row.Would, row.Why = "ignored", why
		case spec.FixesKind(e.Kind):
			row.Would = "worked on"
		default:
			row.Would = "reported"
		}
		rows[e.Workspace] = append(rows[e.Workspace], row)
	}

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"since": since, "workspaces": rows})
		return
	}
	if len(rows) == 0 {
		fmt.Printf("Nothing recorded since %s — the watch has seen no events to replay.\n",
			since.Local().Format("Mon 2 Jan 15:04"))
		return
	}
	fmt.Printf("Since %s, with the rules as they are now\n", since.Local().Format("Mon 2 Jan 15:04"))
	for _, id := range sortedKeys(rows) {
		list := rows[id]
		fmt.Printf("\n%s — %s\n", id, replayTally(list))
		for _, r := range list {
			if r.Would == "ignored" {
				continue
			}
			fmt.Printf("  %-10s %-14s %s\n", r.Would, r.Kind, r.Ref)
		}
		if n := countWould(list, "ignored"); n > 0 {
			fmt.Printf("  %-10s %d — run with --json to see each reason\n", "ignored", n)
		}
	}
	fmt.Println("\nCaps and quiet hours are not replayed: they depend on when a run started,")
	fmt.Println("and a week of real timing is not something a replay can invent.")
}

func replayTally(list []replayRow) string {
	var parts []string
	for _, would := range []string{"worked on", "reported", "ignored"} {
		if n := countWould(list, would); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, would))
		}
	}
	if len(parts) == 0 {
		return "nothing arrived"
	}
	return strings.Join(parts, " · ")
}

func countWould(list []replayRow, would string) int {
	n := 0
	for _, r := range list {
		if r.Would == would {
			n++
		}
	}
	return n
}

func sortedKeys(m map[string][]replayRow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func init() {
	agentWatchReplayCmd.Flags().String("since", "168h", "How far back to replay")
	agentWatchReplayCmd.Flags().String("auto-for", "", "Try this --auto-for setting without saving it")
	agentWatchCmd.AddCommand(agentWatchReplayCmd)
}
