package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

const factoryDefaultCap = 5

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
	{5, "Lights out", "you write specs and scenarios; low-risk work that passes merges with nobody reading it, the rest waits for you"},
}

type factoryFacts struct {
	Watched     bool
	Fix         bool
	Isolate     bool
	AutoMerge   bool
	MergeUnread bool
	Capped      bool
	Reviewed    bool
	DoneWhen    int
	Routines    int
	Scenarios   int
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
	case !f.MergeUnread || f.Scenarios == 0:
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
		{"a person reads what merges", !f.MergeUnread, fmt.Sprintf("corgi agent watch enable %s --merge-unread=false", flag)},
	}
	if (f.AutoMerge || f.MergeUnread) && f.DoneWhen == 0 {
		r.Warnings = append(r.Warnings, "it merges by itself with no checks of its own: add --done-when, or turn --auto-merge off")
	}
	if f.AutoMerge && !f.MergeUnread && f.Scenarios == 0 {
		r.Warnings = append(r.Warnings, "it merges by itself and its only exam is one it can read: add scenarios before trusting that")
	}
	if f.MergeUnread && f.Scenarios == 0 {
		r.Warnings = append(r.Warnings, "with no scenarios nothing passes the gate: every pull request waits for you. corgi agent scenario add <name> --text ...")
	}
	if f.Fix && !f.Capped {
		r.Warnings = append(r.Warnings, "unattended runs have no cap: one bad loop spends the whole window")
	}
	switch level {
	case 1:
		r.Next = []string{fmt.Sprintf("corgi agent level 2 %s", flag), "corgi agent restart"}
	case 2:
		r.Next = []string{fmt.Sprintf("corgi agent level 3 %s", flag), "add one trigger at a time; read every pull request it opens"}
	case 3:
		r.Next = []string{fmt.Sprintf(`corgi agent level 4 %s --done-when "<test command>"`, flag)}
	case 4:
		if f.Routines == 0 {
			r.Next = append(r.Next, "corgi agent routine add retro", "corgi agent routine add ratchet")
		}
		if f.Scenarios == 0 {
			r.Next = append(r.Next, `corgi agent scenario add <name> --text "what a person does and sees"`, "corgi agent routine add scenarios")
		}
		if f.Scenarios > 0 && !f.MergeUnread {
			r.Next = append(r.Next, "when `corgi agent scorecard` and the scenarios have earned it: corgi agent level 5 "+flag)
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
	f.MergeUnread = wc.MergeUnread
	f.DoneWhen = len(wc.DoneWhen)
	f.Capped = wc.MaxFixesPerDay > 0 || wc.MaxFixesTotal > 0 || wc.LimitCeiling > 0
	f.Reviewed = wc.PlanReview != ""
	return f, nil
}

// setFactoryLevel moves one workspace's watch settings to a rung. The level is
// not stored: it stays what these settings add up to, so `watch enable` and this
// can never disagree. A rung whose brake is missing is refused, never half-set.
func setFactoryLevel(ws string, wc *config.WatchConfig, level, scenarios int) (*config.WatchConfig, error) {
	if level < 1 || level > len(factoryLadder) {
		return nil, fmt.Errorf("level is 1 to %d", len(factoryLadder))
	}
	if wc == nil {
		wc = &config.WatchConfig{}
	}
	next := *wc
	if level >= 4 && len(next.DoneWhen) == 0 {
		return nil, fmt.Errorf(`level %d needs checks a run must pass: corgi agent level %d --workspace %s --done-when "<test command>"`, level, level, ws)
	}
	if level == 5 && scenarios == 0 {
		return nil, fmt.Errorf(`level 5 merges by itself, so it needs stories it cannot read: corgi agent scenario add <name> --workspace %s --text "what a person does and sees"`, ws)
	}
	next.Enabled = level >= 2
	if level >= 2 {
		next.Action = "notify"
	}
	if level >= 3 {
		next.Action = "fix"
		if next.MaxFixesPerDay == 0 && next.MaxFixesTotal == 0 && next.LimitCeiling == 0 {
			next.MaxFixesPerDay = factoryDefaultCap
		}
	}
	next.Isolate = level >= 4
	next.AutoMerge = level == 5
	next.MergeUnread = level == 5
	return &next, nil
}

func factoryLevelsOfAll(dir string) ([]factoryReport, error) {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	out := []factoryReport{}
	for _, w := range registry.Sorted() {
		facts, err := factoryFactsFor(dir, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, factoryReportFor(w.ID, facts))
	}
	return out, nil
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
	Use:   "level [1-5] [--workspace <id>] [--all]",
	Short: "How much of a workspace runs without you: see it, or set it for this workspace",
	Long: `corgi works anywhere from a skill you call by hand to a factory that merges what
passes, and each workspace stands on its own rung. With no number this reads the
workspace's settings, says where it stands and names the next step. With a number
it moves that workspace there, and only that one.

  corgi agent level                               this workspace
  corgi agent level --all                         every workspace
  corgi agent level 3                             take tickets to draft pull requests here
  corgi agent level 4 --done-when "go test ./..."
  corgi agent level 1 --workspace payments        stop the watch there

Going up refuses a rung whose brake is missing: 4 needs --done-when, 5 needs a
scenario. Going down turns the accelerators off and keeps your checks, caps and
tracker settings. Restart the daemon to apply it: corgi agent restart.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		if all, _ := cmd.Flags().GetBool("all"); all {
			if len(args) > 0 {
				return fmt.Errorf("a level is set one workspace at a time: leave out --all")
			}
			reports, err := factoryLevelsOfAll(dir)
			if err != nil {
				return err
			}
			if utils.JSONOutput {
				utils.PrintJSON(reports)
				return nil
			}
			for _, r := range reports {
				fmt.Printf("%d  %-24s %s\n", r.Level, r.Workspace, r.Name)
			}
			return nil
		}
		ws, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		doneWhen, _ := cmd.Flags().GetStringSlice("done-when")
		if len(args) == 0 {
			if len(doneWhen) > 0 {
				return fmt.Errorf("--done-when goes with a level: corgi agent level 4 --done-when %q", strings.Join(doneWhen, ","))
			}
		} else if err := moveWorkspaceToLevel(dir, ws, args[0], doneWhen); err != nil {
			return err
		}
		facts, err := factoryFactsFor(dir, ws)
		if err != nil {
			return err
		}
		r := factoryReportFor(ws, facts)
		if utils.JSONOutput {
			utils.PrintJSON(r)
			return nil
		}
		printFactoryReport(r)
		if len(args) > 0 {
			fmt.Println("\nRestart the daemon to apply it: corgi agent restart")
		}
		return nil
	},
}

func moveWorkspaceToLevel(dir, ws, arg string, doneWhen []string) error {
	level, err := strconv.Atoi(arg)
	if err != nil {
		return fmt.Errorf("%q is not a level: 1 to %d", arg, len(factoryLadder))
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	entry := user.Workspaces[ws]
	wc := entry.Watch
	if len(doneWhen) > 0 {
		if wc == nil {
			wc = &config.WatchConfig{}
		}
		withChecks := *wc
		withChecks.DoneWhen = doneWhen
		wc = &withChecks
	}
	next, err := setFactoryLevel(ws, wc, level, len(watch.LoadScenarios(dir, ws)))
	if err != nil {
		return err
	}
	entry.Watch = next
	if user.Workspaces == nil {
		user.Workspaces = map[string]config.WorkspaceConfig{}
	}
	user.Workspaces[ws] = entry
	return writeUserConfig(path, user)
}

func init() {
	agentLevelCmd.Flags().String("workspace", "", "the workspace (default: the one you are in)")
	agentLevelCmd.Flags().Bool("all", false, "every workspace and its level")
	agentLevelCmd.Flags().StringSlice("done-when", nil, "with a level: the commands a run must pass here, comma separated")
	agentCmd.AddCommand(agentLevelCmd)
}
