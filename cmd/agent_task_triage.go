package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

var agentTaskTriageCmd = &cobra.Command{
	Use:   "triage",
	Short: "The tasks that wait for your yes: accept, snooze or drop each one",
	Long: `A task an agent, a guest or a sweep put on the board waits here first.
Nothing starts on it until you accept it.

  corgi agent task triage
  corgi agent task accept TASK-7
  corgi agent task snooze TASK-7 --for 3d
  corgi agent task move TASK-7 Canceled      (drop it)`,
	Args: cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		queue := watch.LoadTasks(mustAgentDir()).TriageQueue(time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"triage": queue})
			return
		}
		if len(queue) == 0 {
			fmt.Println("nothing waits for triage")
			return
		}
		for _, t := range queue {
			fmt.Printf("%-8s %s  (from %s)\n", t.Ref(), t.Title, firstNonEmptyString(t.By, "someone"))
		}
	},
}

var agentTaskAcceptCmd = &cobra.Command{
	Use:   "accept <TASK-n>...",
	Short: "Take tasks out of triage: they join the queue",
	Args:  cobra.MinimumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		dir := mustAgentDir()
		tasks, now := watch.LoadTasks(dir), time.Now()
		for _, arg := range args {
			t, err := tasks.Accept(arg, now)
			taskChanged(dir, t, err, "accepted")
		}
	},
}

var agentTaskSnoozeCmd = &cobra.Command{
	Use:   "snooze <TASK-n>",
	Short: "Put a task to sleep: it leaves triage and no run starts on it until then",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		now := time.Now()
		until, err := snoozeUntil(cmd, now)
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		t, err := watch.LoadTasks(dir).Snooze(args[0], until, now)
		taskChanged(dir, t, err, "sleeps until "+until.Local().Format("Jan 2 15:04"))
	},
}

func snoozeUntil(cmd *cobra.Command, now time.Time) (time.Time, error) {
	if day, _ := cmd.Flags().GetString("until"); strings.TrimSpace(day) != "" {
		at, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), now.Location())
		if err != nil {
			return time.Time{}, fmt.Errorf("--until is a day like 2026-12-01")
		}
		return at, nil
	}
	raw, _ := cmd.Flags().GetString("for")
	lasts, err := parseLinkFor(raw)
	if err != nil || lasts <= 0 {
		return time.Time{}, fmt.Errorf("--for is like 30m, 12h or 7d")
	}
	return now.Add(lasts), nil
}

var agentCycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "A box of days for a set of tasks: what is in, what is done, what slipped",
	Long: `A cycle is a week (or any number of days) and the tasks you mean to finish
in it. Closing it records what was done and takes the rest out, as slipped.

  corgi agent cycle start --days 7
  corgi agent cycle add TASK-3 TASK-4
  corgi agent cycle                    where it stands
  corgi agent cycle close
  corgi agent cycle history`,
	Args: cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		dir := mustAgentDir()
		current, open := watch.LoadCycles(dir).Current()
		if !open {
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"cycle": nil})
				return
			}
			fmt.Println("no cycle is open - corgi agent cycle start")
			return
		}
		report := cycleReport(dir, current, time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(report)
			return
		}
		fmt.Println(report.line())
		for _, row := range report.Tasks {
			fmt.Printf("  %-8s %-8s %s\n", row.Ref, row.State, row.Title)
		}
	},
}

type cycleTask struct {
	Ref   string `json:"ref"`
	Title string `json:"title"`
	State string `json:"state"`
	Owner string `json:"owner,omitempty"`
}

type cycleStanding struct {
	Cycle    watch.Cycle `json:"cycle"`
	DaysLeft int         `json:"daysLeft"`
	Total    int         `json:"total"`
	Done     int         `json:"done"`
	Doing    int         `json:"doing"`
	Tokens   int64       `json:"tokens,omitempty"`
	USD      float64     `json:"usd,omitempty"`
	Tasks    []cycleTask `json:"tasks"`
}

func (r cycleStanding) line() string {
	left := fmt.Sprintf("%d day(s) left", r.DaysLeft)
	if r.DaysLeft <= 0 {
		left = "past its end"
	}
	cost := ""
	if r.USD > 0 {
		cost = fmt.Sprintf(" · $%.2f", r.USD)
	}
	return fmt.Sprintf("%s  %s → %s  %d of %d done, %d in work · %s%s", r.Cycle.Name, r.Cycle.Start, r.Cycle.End, r.Done, r.Total, r.Doing, left, cost)
}

func cycleReport(dir string, c watch.Cycle, now time.Time) cycleStanding {
	out := cycleStanding{Cycle: c, DaysLeft: c.DaysLeft(now), Tasks: []cycleTask{}}
	costs := map[string]*CardCost{}
	for _, card := range gatherKanban(dir, "", now) {
		costs[card.Ref] = card.Cost
	}
	for _, t := range watch.LoadTasks(dir).InCycle(c.Name) {
		out.Total++
		switch {
		case watch.FinishedState(t.State) != "":
			out.Done++
		case t.State != watch.TaskColumns[0]:
			out.Doing++
		}
		if cost := costs[t.Ref()]; cost != nil {
			out.Tokens += cost.Tokens
			out.USD += cost.USD
		}
		out.Tasks = append(out.Tasks, cycleTask{Ref: t.Ref(), Title: t.Title, State: t.State, Owner: t.Owner})
	}
	return out
}

var agentCycleStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Open a cycle from today",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		name, _ := cmd.Flags().GetString("name")
		days, _ := cmd.Flags().GetInt("days")
		c, err := watch.LoadCycles(mustAgentDir()).Start(name, days, time.Now())
		if err != nil {
			exitWithError("agent_cycle", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"cycle": c})
			return
		}
		fmt.Printf("cycle %s: %s → %s\n", c.Name, c.Start, c.End)
	},
}

func changeCycle(args []string, name string) {
	dir := mustAgentDir()
	tasks, now := watch.LoadTasks(dir), time.Now()
	for _, arg := range args {
		t, err := tasks.SetCycle(arg, name, now)
		said := "is in " + name
		if name == "" {
			said = "is out of the cycle"
		}
		taskChanged(dir, t, err, said)
	}
}

var agentCycleAddCmd = &cobra.Command{
	Use:   "add <TASK-n>...",
	Short: "Put tasks in the open cycle",
	Args:  cobra.MinimumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		current, open := watch.LoadCycles(mustAgentDir()).Current()
		if !open {
			exitWithError("agent_cycle", fmt.Errorf("no cycle is open - corgi agent cycle start"), 2)
		}
		changeCycle(args, current.Name)
	},
}

var agentCycleRmCmd = &cobra.Command{
	Use:   "rm <TASK-n>...",
	Short: "Take tasks out of their cycle",
	Args:  cobra.MinimumNArgs(1),
	Run:   func(_ *cobra.Command, args []string) { changeCycle(args, "") },
}

var agentCycleCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "End the open cycle: finished tasks are recorded, the rest slip out",
	Args:  cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		dir := mustAgentDir()
		c, err := watch.LoadCycles(dir).Close(watch.LoadTasks(dir), time.Now())
		if err != nil {
			exitWithError("agent_cycle", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"cycle": c})
			return
		}
		fmt.Printf("cycle %s closed: %d done, %d slipped", c.Name, len(c.Done), len(c.Slipped))
		if len(c.Slipped) > 0 {
			fmt.Printf(" (%s)", strings.Join(c.Slipped, ", "))
		}
		fmt.Println()
	},
}

var agentCycleHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Every cycle so far: done and slipped",
	Args:  cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		all := watch.LoadCycles(mustAgentDir()).All()
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"cycles": all})
			return
		}
		if len(all) == 0 {
			fmt.Println("no cycles yet - corgi agent cycle start")
			return
		}
		for _, c := range all {
			state := "open"
			if !c.Open() {
				state = fmt.Sprintf("%d done, %d slipped", len(c.Done), len(c.Slipped))
			}
			fmt.Printf("%-12s %s → %s  %s\n", c.Name, c.Start, c.End, state)
		}
	},
}

func init() {
	agentTaskSnoozeCmd.Flags().String("for", "1d", "how long: 30m, 12h, 3d")
	agentTaskSnoozeCmd.Flags().String("until", "", "or the day it wakes, like 2026-12-01")
	agentTaskCmd.AddCommand(agentTaskTriageCmd, agentTaskAcceptCmd, agentTaskSnoozeCmd)
	agentCycleStartCmd.Flags().String("name", "", "the cycle's name (default: the ISO week, like 2026-W47)")
	agentCycleStartCmd.Flags().Int("days", 7, "how many days it lasts, today included")
	agentCycleCmd.AddCommand(agentCycleStartCmd, agentCycleAddCmd, agentCycleRmCmd, agentCycleCloseCmd, agentCycleHistoryCmd)
	agentCmd.AddCommand(agentCycleCmd)
}
