package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var agentWhileAwayCmd = &cobra.Command{
	Use:   "while-away",
	Short: "What happened while you were gone",
	Long: `The one card worth reading at the start of the day: what arrived, what corgi
worked on and opened, what it could not do, and what is still waiting.

  corgi agent while-away             # since you last looked
  corgi agent while-away --since 12h
  corgi agent while-away --json

Quiet hours hold notifications and release one summary; that summary is gone
the moment it buzzes. This is the same thing, still there in the morning.`,
	Run: runAgentWhileAway,
}

type awayReport struct {
	Since    time.Time         `json:"since"`
	Arrived  []awayLine        `json:"arrived,omitempty"`
	Opened   []awayLine        `json:"opened,omitempty"`
	Failed   []awayLine        `json:"failed,omitempty"`
	Waiting  []awayLine        `json:"waiting,omitempty"`
	Deferred map[string]string `json:"deferred,omitempty"`
}

type awayLine struct {
	Ref       string `json:"ref"`
	Workspace string `json:"workspace,omitempty"`
	What      string `json:"what,omitempty"`
	URL       string `json:"url,omitempty"`
}

func runAgentWhileAway(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	sinceFlag, _ := cmd.Flags().GetString("since")
	dur, err := time.ParseDuration(sinceFlag)
	if err != nil {
		exitWithError("agent_while_away", fmt.Errorf("--since takes a duration like 12h, not %q", sinceFlag), 2)
	}
	since := time.Now().Add(-dur)
	rep := awayReport{Since: since, Deferred: map[string]string{}}

	state := watch.LoadState(dir)
	for _, e := range watch.EventsSince(dir, since) {
		if state.IsIgnored(e.Key) {
			continue
		}
		rep.Arrived = append(rep.Arrived, awayLine{Ref: firstNonEmptyString(e.Ref, e.Key),
			Workspace: e.Workspace, What: string(e.Kind), URL: e.URL})
	}

	log := watch.LoadFixLog(dir)
	for _, r := range log.FixesSince(since) {
		line := awayLine{Ref: firstNonEmptyString(r.Ref, r.Key), Workspace: r.Workspace, URL: r.URL}
		switch {
		case !r.Done():
			line.What = "still running"
			rep.Waiting = append(rep.Waiting, line)
		case r.Error != "":
			line.What = firstLineOf(r.Error)
			rep.Failed = append(rep.Failed, line)
		case len(r.PRs) > 0:
			line.What, line.URL = r.Outcome(), r.PRs[0]
			rep.Opened = append(rep.Opened, line)
		}
	}
	for _, e := range log.DeferredEvents() {
		rep.Deferred[firstNonEmptyString(e.Ref, e.Key)] = e.Workspace
	}

	if utils.JSONOutput {
		utils.PrintJSON(rep)
		return
	}
	printWhileAway(rep)
}

func printWhileAway(rep awayReport) {
	fmt.Printf("Since %s\n", rep.Since.Local().Format("Mon 2 Jan 15:04"))
	if len(rep.Arrived)+len(rep.Opened)+len(rep.Failed)+len(rep.Waiting)+len(rep.Deferred) == 0 {
		fmt.Println("  nothing arrived, and corgi did nothing on its own — a quiet one")
		return
	}
	block := func(title string, lines []awayLine) {
		if len(lines) == 0 {
			return
		}
		fmt.Printf("\n%s\n", title)
		for _, l := range lines {
			where := ""
			if l.Workspace != "" {
				where = " (" + l.Workspace + ")"
			}
			fmt.Printf("  %-28s %s%s\n", l.Ref, l.What, where)
			if l.URL != "" {
				fmt.Printf("  %-28s %s\n", "", l.URL)
			}
		}
	}
	block("corgi opened", rep.Opened)
	block("corgi could not", rep.Failed)
	block("still running", rep.Waiting)
	block("arrived", rep.Arrived)
	if len(rep.Deferred) > 0 {
		refs := make([]string, 0, len(rep.Deferred))
		for ref := range rep.Deferred {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		fmt.Printf("\nwaiting for a free slot\n  %s\n", strings.Join(refs, ", "))
		fmt.Println("  a cap or quiet hours held these; corgi agent watch run picks them up")
	}
}

func init() {
	agentWhileAwayCmd.Flags().String("since", "12h", "How far back to look")
	agentCmd.AddCommand(agentWhileAwayCmd)
}
