package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/lessons"
)

var agentLessonCmd = &cobra.Command{
	Use:   "lesson",
	Short: "What this workspace learned the hard way - read by every new session",
	Long: `A lesson is one line: the review that said line 40 was wrong, the check
that stayed red, the rule nobody wrote down. They live in
<agentDir>/lessons/<workspace>.md, outside the repository, and the SessionStart
context hook tells a new session how many there are and where.

  corgi agent lesson add "never mock the database in api tests"
  corgi agent lesson list
  corgi agent watch enable --lessons     # the daemon writes red gates and failed bots`,
}

var agentLessonAddCmd = &cobra.Command{
	Use:   "add <text>",
	Short: "Write one lesson for this workspace",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		if ws == "" {
			id, err := currentWorkspaceID(dir)
			if err != nil {
				exitWithError("agent_lesson", err, 1)
			}
			ws = id
		}
		if err := lessons.Add(dir, ws, lessons.Lesson{Source: "you", Text: strings.Join(args, " ")}); err != nil {
			exitWithError("agent_lesson", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"done": "written", "path": lessons.Path(dir, ws), "count": len(lessons.List(dir, ws))})
			return
		}
		fmt.Printf("Written to %s\n", lessons.Path(dir, ws))
	},
}

var agentLessonListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every lesson for this workspace, oldest first",
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		if ws == "" {
			id, err := currentWorkspaceID(dir)
			if err != nil {
				exitWithError("agent_lesson", err, 1)
			}
			ws = id
		}
		list := lessons.List(dir, ws)
		if utils.JSONOutput {
			if list == nil {
				list = []lessons.Lesson{}
			}
			utils.PrintJSON(map[string]any{"workspace": ws, "path": lessons.Path(dir, ws), "lessons": list})
			return
		}
		if len(list) == 0 {
			fmt.Printf("No lessons for %s yet. corgi agent lesson add \"…\", or corgi agent watch enable --lessons\n", ws)
			return
		}
		for _, l := range list {
			line := ""
			if !l.At.IsZero() {
				line += l.At.Format("2006-01-02") + " · "
			}
			if l.Source != "" {
				line += l.Source + ": "
			}
			fmt.Println(line + l.Text)
		}
	},
}

var agentLessonDreamCmd = &cobra.Command{
	Use:   "dream",
	Short: "Tidy every workspace's lessons: merge near-duplicates, put away old ones (runs weekly by itself)",
	Long: `A dreaming pass over the lessons: a lesson that says what a newer one says
is merged into it, one older than 180 days or past the newest 60 is put
away. Nothing is lost - what was taken out goes to lessons/<workspace>.dreamed.md
with the reason. Lines you wrote by hand stay.

  corgi agent lesson dream --dry-run   what it would take out
  corgi agent lesson dream`,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		opt := lessons.DefaultDream
		opt.DryRun, _ = cmd.Flags().GetBool("dry-run")
		reports, err := lessons.DreamAll(dir, time.Now(), opt)
		if err != nil {
			exitWithError("agent_lesson", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(reports)
			return
		}
		if len(reports) == 0 {
			fmt.Println("No lessons yet.")
			return
		}
		verb := "put away"
		if opt.DryRun {
			verb = "would put away"
		}
		for _, r := range reports {
			fmt.Printf("%s: %d lessons, %s %d\n", r.Workspace, r.Before, verb, len(r.Dropped))
			for _, d := range r.Dropped {
				fmt.Printf("  - %s  (%s)\n", d.Lesson.Text, d.Why)
			}
		}
	},
}

func init() {
	agentLessonDreamCmd.Flags().Bool("dry-run", false, "say what would be put away, change nothing")
	agentLessonAddCmd.Flags().String("workspace", "", "The workspace (default: the one this directory is in)")
	agentLessonListCmd.Flags().String("workspace", "", "The workspace (default: the one this directory is in)")
	agentLessonCmd.AddCommand(agentLessonAddCmd, agentLessonListCmd, agentLessonDreamCmd)
	agentCmd.AddCommand(agentLessonCmd)
}
