package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/lessons"
)

var agentLessonCmd = &cobra.Command{
	Use:   "lesson",
	Short: "What this workspace learned the hard way — read by every new session",
	Long: `A lesson is one line: the review that said line 40 was wrong, the check
that stayed red, the rule nobody wrote down. They live in
<agentDir>/lessons/<workspace>.md, outside the repository, and the SessionStart
context hook tells a new session how many there are and where.

  corgi agent lesson add "never mock the database in api tests"
  corgi agent lesson list
  corgi agent watch enable --lessons     # the daemon writes reviews, red gates, failed bots`,
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

func init() {
	agentLessonAddCmd.Flags().String("workspace", "", "The workspace (default: the one this directory is in)")
	agentLessonListCmd.Flags().String("workspace", "", "The workspace (default: the one this directory is in)")
	agentLessonCmd.AddCommand(agentLessonAddCmd, agentLessonListCmd)
	agentCmd.AddCommand(agentLessonCmd)
}
