package cmd

import (
	"fmt"
	"os"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

type factoryRung struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
	You   string `json:"you"`
}

var factoryLadder = []factoryRung{
	{1, "You drive", "you start each session; skills help on demand"},
	{2, "It tells you", "the laptop watches trackers and pull requests and rings; you start the work"},
	{3, "It drafts, you review", "agents take tickets into draft pull requests; you read every diff"},
	{4, "It works a shift", "you write specs and checks; agents run in worktrees, on a clock, in plans; you read pull requests"},
	{5, "Lights out", "you write specs and scenarios; what passes merges by itself"},
}

type factoryFacts struct {
	Watched   bool
	Fix       bool
	Isolate   bool
	AutoMerge bool
	Capped    bool
	Reviewed  bool
	DoneWhen  int
	Routines  int
	Scenarios int
}

type factoryBrake struct {
	Name string `json:"name"`
	On   bool   `json:"on"`
	Turn string `json:"turn,omitempty"`
}

type factoryReport struct {
	Workspace string         `json:"workspace"`
	Level     int            `json:"level"`
	Name      string         `json:"name"`
	You       string         `json:"you"`
	Brakes    []factoryBrake `json:"brakes"`
	Warnings  []string       `json:"warnings,omitempty"`
	Next      []string       `json:"next,omitempty"`
}

func factoryLevel(f factoryFacts) int {
	switch {
	case !f.Watched:
		return 1
	case !f.Fix:
		return 2
	case !f.Isolate || f.DoneWhen == 0:
		return 3
	case !f.AutoMerge || f.Scenarios == 0:
		return 4
	}
	return 5
}

func factoryReportFor(ws string, f factoryFacts) factoryReport {
	level := factoryLevel(f)
	rung := factoryLadder[level-1]
	r := factoryReport{Workspace: ws, Level: level, Name: rung.Name, You: rung.You}
	flag := "--workspace " + ws
	r.Brakes = []factoryBrake{
		{"checks a run must pass", f.DoneWhen > 0, fmt.Sprintf(`corgi agent watch enable %s --done-when "<test command>"`, flag)},
		{"a person signs off risky plans", f.Reviewed, fmt.Sprintf(`corgi agent watch enable %s --plan-review "risk>=7"`, flag)},
		{"stories it cannot see", f.Scenarios > 0, `corgi agent scenario add <name> --text "what a person does and sees"`},
		{"a cap on runs and spend", f.Capped, fmt.Sprintf("corgi agent watch enable %s --max-per-day 10 --limit-ceiling 80", flag)},
		{"a person merges", !f.AutoMerge, fmt.Sprintf("corgi agent watch enable %s --auto-merge=false", flag)},
	}
	if f.AutoMerge && f.DoneWhen == 0 {
		r.Warnings = append(r.Warnings, "it merges by itself with no checks of its own: add --done-when, or turn --auto-merge off")
	}
	if f.AutoMerge && f.Scenarios == 0 {
		r.Warnings = append(r.Warnings, "it merges by itself and its only exam is one it can read: add scenarios before trusting that")
	}
	if f.Fix && !f.Capped {
		r.Warnings = append(r.Warnings, "unattended runs have no cap: one bad loop spends the whole window")
	}
	switch level {
	case 1:
		r.Next = []string{fmt.Sprintf("corgi agent watch enable %s", flag), "corgi agent restart"}
	case 2:
		r.Next = []string{fmt.Sprintf("corgi agent watch enable %s --action fix --max-per-day 5", flag), "add one trigger at a time; read every pull request it opens"}
	case 3:
		if !f.Isolate {
			r.Next = append(r.Next, fmt.Sprintf("corgi agent watch enable %s --isolate", flag))
		}
		if f.DoneWhen == 0 {
			r.Next = append(r.Next, fmt.Sprintf(`corgi agent watch enable %s --done-when "<test command>"`, flag))
		}
	case 4:
		if f.Routines == 0 {
			r.Next = append(r.Next, "corgi agent routine add retro", "corgi agent routine add ratchet")
		}
		if f.Scenarios == 0 {
			r.Next = append(r.Next, `corgi agent scenario add <name> --text "what a person does and sees"`, "corgi agent routine add scenarios")
		}
		if f.Scenarios > 0 && !f.AutoMerge {
			r.Next = append(r.Next, "when `corgi agent scorecard` and the scenarios have earned it: corgi agent watch enable "+flag+" --auto-merge")
		}
	case 5:
		r.Next = []string{"keep the loop honest: corgi agent scorecard, corgi agent selfcheck, corgi agent routine add retro"}
	}
	return r
}

func factoryFactsFor(dir, ws string) (factoryFacts, error) {
	f := factoryFacts{Scenarios: len(watch.LoadScenarios(dir, ws))}
	r, err := resolveWorkspaceConfig(dir, ws)
	if err != nil {
		return f, err
	}
	f.Routines = len(r.Routines)
	wc := r.Watch
	if wc == nil || !wc.Enabled {
		return f, nil
	}
	f.Watched = true
	f.Fix = wc.Action == "fix"
	f.Isolate = wc.Isolate
	f.AutoMerge = wc.AutoMerge
	f.DoneWhen = len(wc.DoneWhen)
	f.Capped = wc.MaxFixesPerDay > 0 || wc.MaxFixesTotal > 0 || wc.LimitCeiling > 0
	f.Reviewed = wc.PlanReview != ""
	return f, nil
}

func printFactoryReport(r factoryReport) {
	fmt.Printf("%s is at level %d of %d: %s\n\n", r.Workspace, r.Level, len(factoryLadder), r.Name)
	for _, rung := range factoryLadder {
		mark := "  "
		if rung.Level == r.Level {
			mark = "> "
		}
		fmt.Printf("%s%d  %-22s %s\n", mark, rung.Level, rung.Name, rung.You)
	}
	fmt.Println("\nBrakes")
	for _, b := range r.Brakes {
		word := "on "
		tail := ""
		if !b.On {
			word = "off"
			tail = "  (" + b.Turn + ")"
		}
		fmt.Printf("  %s  %s%s\n", word, b.Name, tail)
	}
	if len(r.Warnings) > 0 {
		fmt.Println("\nWatch out")
		for _, w := range r.Warnings {
			fmt.Printf("  %s\n", w)
		}
	}
	if len(r.Next) > 0 {
		fmt.Println("\nNext")
		fmt.Printf("  %s\n", strings.Join(r.Next, "\n  "))
	}
}

var agentLevelCmd = &cobra.Command{
	Use:   "level [--workspace <id>]",
	Short: "How much of this workspace runs without you, which brakes are on, and the next step",
	Long: `corgi works anywhere from a skill you call by hand to a factory that merges what
passes. This reads the workspace's settings, says where it stands, and names the
next step. It changes nothing. Every step up is one more trigger: turn on a brake
before the trigger that needs it.

  corgi agent level
  corgi agent level --workspace api --json`,
	Run: func(cmd *cobra.Command, _ []string) {
		ws, _ := cmd.Flags().GetString("workspace")
		if ws == "" {
			if cwd, err := os.Getwd(); err == nil {
				_, ws = workspaceRootFor(cwd)
			}
		}
		if ws == "" {
			exitWithError("agent_level", fmt.Errorf("say which workspace: --workspace <id>, or run inside one"), 2)
		}
		facts, err := factoryFactsFor(mustAgentDir(), ws)
		if err != nil {
			exitWithError("agent_level", err, 2)
		}
		r := factoryReportFor(ws, facts)
		if utils.JSONOutput {
			utils.PrintJSON(r)
			return
		}
		printFactoryReport(r)
	},
}

func init() {
	agentLevelCmd.Flags().String("workspace", "", "the workspace (default: the one you are in)")
	agentCmd.AddCommand(agentLevelCmd)
}
