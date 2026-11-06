package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

const scenarioTimeout = 15 * time.Minute

const scenarioSoul = `You judge whether software satisfies a person, from a scenario the people who built it never saw.
Read the code, query what is running, run what you are allowed to run. Never change a file.
Judge what a real person would conclude, not whether the code looks right. If you could not exercise the behaviour, say so and answer false.
In "why", describe the behaviour you saw in one sentence, without quoting the scenario: the builder reads it.
End with exactly one line of JSON: {"satisfied": true or false, "why": "..."}`

var scenarioReadTools = []string{"Read", "Grep", "Glob", "Bash(curl:*)", "Bash(corgi status:*)", "Bash(corgi ps:*)", "Bash(corgi logs:*)", "Bash(git log:*)", "Bash(git diff:*)"}

// judgeScenario is one run of the judge, a var so tests stay off the network.
var judgeScenario = func(ctx context.Context, dir, model, prompt string, allow []string) (string, error) {
	return printRun(ctx, dir, dir, harness.Print{
		Prompt: prompt, System: scenarioSoul, Model: model, Text: true,
		AllowedTools:    append(append([]string{}, scenarioReadTools...), allow...),
		DisallowedTools: []string{"Edit", "Write", "NotebookEdit"},
	})
}

func runScenarios(ctx context.Context, scenarios []watch.Scenario, dir, model, ref string, times int, allow []string, now func() time.Time) []watch.ScenarioResult {
	out := make([]watch.ScenarioResult, 0, len(scenarios)*times)
	for _, s := range scenarios {
		for range times {
			prompt := fmt.Sprintf("Scenario %s:\n\n%s", s.Name, s.Text)
			text, err := judgeScenario(ctx, dir, model, prompt, allow)
			satisfied, why := watch.ParseVerdict(text)
			if err != nil {
				satisfied, why = false, "the judge did not finish: "+err.Error()
			}
			out = append(out, watch.ScenarioResult{Scenario: s.Name, Satisfied: satisfied, Why: why, Ref: ref, At: now()})
		}
	}
	return out
}

// judgeCheckout runs every scenario once against a checkout: the share they
// satisfied and how many there were.
func judgeCheckout(ctx context.Context, agentDir, workspace, checkout, ref string) (int, int, error) {
	list := watch.LoadScenarios(agentDir, workspace)
	if len(list) == 0 {
		return 0, 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, scenarioTimeout*time.Duration(len(list)))
	defer cancel()
	results := runScenarios(ctx, list, checkout, "", ref, 1, nil, time.Now)
	if err := watch.AppendScenarioResults(agentDir, workspace, results); err != nil {
		utils.Infof("could not keep the results: %v\n", err)
	}
	return watch.Satisfaction(results), len(list), nil
}

func scenarioWorkspace(cmd *cobra.Command) string {
	ws, _ := cmd.Flags().GetString("workspace")
	if ws == "" {
		if cwd, err := os.Getwd(); err == nil {
			_, ws = workspaceRootFor(cwd)
		}
	}
	if ws == "" {
		exitWithError("agent_scenario", fmt.Errorf("say which workspace: --workspace <id>, or run inside one"), 2)
	}
	return ws
}

var agentScenarioCmd = &cobra.Command{
	Use:   "scenario",
	Short: "End-to-end stories a change is judged against, kept where the session building it does not look",
	Long: `An agent that can read its tests can pass them without doing the work. A
scenario is a story kept outside every repository, in the agent directory. A
separate run reads it, tries the behaviour with read-only tools, and says whether
a person would be satisfied. The result is a share, not a green tick.

  corgi agent scenario add refund --text "A customer who paid yesterday asks for a refund, and sees it on their statement page."
  corgi agent scenario add checkout --file checkout.md
  corgi agent scenario list
  corgi agent scenario run                       every scenario, once, on what is checked out here
  corgi agent scenario run --dir <worktree> --times 3 --min 90
  corgi agent scenario rm refund

The builder's words back are only the judge's one line on what it saw. A session
on this machine could still open the folder if it went looking: this keeps
scenarios out of its way, it is not a lock.`,
}

var agentScenarioAddCmd = &cobra.Command{
	Use:   "add <name> [--text ... | --file ... | stdin]",
	Short: "Write a scenario for this workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ws := scenarioWorkspace(cmd)
		text, _ := cmd.Flags().GetString("text")
		if file, _ := cmd.Flags().GetString("file"); file != "" {
			data, err := os.ReadFile(file)
			if err != nil {
				exitWithError("agent_scenario", err, 2)
			}
			text = string(data)
		}
		if text == "" {
			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				exitWithError("agent_scenario", err, 2)
			}
			text = string(data)
		}
		if err := watch.AddScenario(mustAgentDir(), ws, args[0], text); err != nil {
			exitWithError("agent_scenario", err, 2)
		}
		fmt.Printf("scenario %s kept for %s, outside the repository\n", args[0], ws)
	},
}

var agentScenarioListCmd = &cobra.Command{
	Use:   "list",
	Short: "The scenarios of this workspace",
	Run: func(cmd *cobra.Command, _ []string) {
		ws := scenarioWorkspace(cmd)
		list := watch.LoadScenarios(mustAgentDir(), ws)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": ws, "scenarios": list})
			return
		}
		if len(list) == 0 {
			fmt.Printf("no scenarios for %s: corgi agent scenario add <name> --text \"what a person does and sees\"\n", ws)
			return
		}
		for _, s := range list {
			fmt.Printf("%-24s %s\n", s.Name, scenarioPreview(s.Text, 90))
		}
	},
}

var agentScenarioRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a scenario",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := watch.RemoveScenario(mustAgentDir(), scenarioWorkspace(cmd), args[0]); err != nil {
			exitWithError("agent_scenario", err, 2)
		}
		fmt.Printf("scenario %s removed\n", args[0])
	},
}

var agentScenarioRunCmd = &cobra.Command{
	Use:   "run [name...]",
	Short: "Judge what is checked out against the scenarios; exits 1 under --min",
	Run: func(cmd *cobra.Command, args []string) {
		ws := scenarioWorkspace(cmd)
		agentDir := mustAgentDir()
		list := pickScenarios(watch.LoadScenarios(agentDir, ws), args)
		if len(list) == 0 {
			exitWithError("agent_scenario", fmt.Errorf("no scenarios to run for %s: corgi agent scenario add <name> --text ...", ws), 2)
		}
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			root, err := workspaceRoot(ws)
			if err != nil {
				exitWithError("agent_scenario", err, 2)
			}
			dir = root
		}
		model, _ := cmd.Flags().GetString("model")
		times, _ := cmd.Flags().GetInt("times")
		if times < 1 {
			times = 1
		}
		minimum, _ := cmd.Flags().GetInt("min")
		ref, _ := cmd.Flags().GetString("ref")
		allow, _ := cmd.Flags().GetStringSlice("allow")
		ctx, cancel := context.WithTimeout(context.Background(), scenarioTimeout*time.Duration(len(list)*times))
		defer cancel()
		results := runScenarios(ctx, list, dir, model, ref, times, allow, time.Now)
		if err := watch.AppendScenarioResults(agentDir, ws, results); err != nil {
			utils.Infof("could not keep the results: %v\n", err)
		}
		share := watch.Satisfaction(results)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": ws, "satisfaction": share, "results": results})
		} else {
			fmt.Printf("satisfaction %d%% over %d run(s)\n", share, len(results))
			for _, r := range results {
				word := "ok  "
				if !r.Satisfied {
					word = "miss"
				}
				fmt.Printf("  %s %-24s %s\n", word, r.Scenario, r.Why)
			}
		}
		if share < minimum {
			os.Exit(1)
		}
	},
}

func pickScenarios(all []watch.Scenario, names []string) []watch.Scenario {
	if len(names) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []watch.Scenario
	for _, s := range all {
		if want[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

func scenarioPreview(text string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > limit {
		return line[:limit-1] + "…"
	}
	return line
}

func init() {
	for _, c := range []*cobra.Command{agentScenarioAddCmd, agentScenarioListCmd, agentScenarioRmCmd, agentScenarioRunCmd} {
		c.Flags().String("workspace", "", "the workspace (default: the one you are in)")
		agentScenarioCmd.AddCommand(c)
	}
	agentScenarioAddCmd.Flags().String("text", "", "the scenario, in words")
	agentScenarioAddCmd.Flags().String("file", "", "read the scenario from a file")
	agentScenarioRunCmd.Flags().String("dir", "", "where to judge: a worktree or a branch checked out (default: the workspace)")
	agentScenarioRunCmd.Flags().String("model", "sonnet", "the judge's model")
	agentScenarioRunCmd.Flags().Int("times", 1, "runs per scenario; more than one gives a truer share")
	agentScenarioRunCmd.Flags().Int("min", 0, "exit 1 under this satisfaction percent")
	agentScenarioRunCmd.Flags().String("ref", "", "the ticket or branch these results belong to")
	agentScenarioRunCmd.Flags().StringSlice("allow", nil, "more tools the judge may use, e.g. 'Bash(maestro:*)'")
	agentCmd.AddCommand(agentScenarioCmd)
}
