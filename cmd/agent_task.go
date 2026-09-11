package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

// A task of your own on the board: written for later, picked up when
// there is time, moved along by the session that works on it.

var agentTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Tasks of your own on the board: add one for later, pick it up, move it along",
	Long: `A task is a ticket you write yourself — a title, a description, the
workspace it is for — kept on this machine and shown on the same board as the
tracker's tickets: the inbox, the Board tab, the page, the menu bar. It goes
through Todo, Doing, Review, Done (or Canceled), and nothing about it reaches
a tracker.

  corgi agent task add "Meta SDK on iOS" --workspace app --body "App Events, not the pixel"
  corgi agent task list
  corgi agent watch work TASK-3           a session on it: the description is the prompt
  corgi agent task move TASK-3 Review     the session runs this when a draft PR is up
  corgi agent task done TASK-3

"Work on it" on the phone or the page is the same as watch work: the task
moves to Doing at once, the board says a session is on its way, and from the
session's first event the card names it.`,
}

var agentTaskAddCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Put a task on the board, in Todo",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		body, _ := cmd.Flags().GetString("body")
		ws, _ := cmd.Flags().GetString("workspace")
		if body == "-" {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				exitWithError("agent_task", err, 1)
			}
			body = string(raw)
		}
		if ws = strings.TrimSpace(ws); ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				exitWithError("agent_task", fmt.Errorf("no such workspace: %s", ws), 2)
			}
		} else if cwd, err := os.Getwd(); err == nil {
			ws = workspaceIDForDir(dir, cwd)
		}
		t, err := watch.LoadTasks(dir).Add(strings.Join(args, " "), body, ws, "cli", time.Now())
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ref": t.Ref(), "key": t.Key(), "task": t})
			return
		}
		where := ""
		if t.Workspace != "" {
			where = " in " + t.Workspace
		}
		fmt.Printf("%s on the board%s: %s\n", t.Ref(), where, t.Title)
	},
}

var agentTaskListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every task on the board, by column",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		all, _ := cmd.Flags().GetBool("all")
		tasks := watch.LoadTasks(dir).Tasks
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"tasks": tasks, "columns": watch.TaskColumns})
			return
		}
		if len(tasks) == 0 {
			fmt.Println("no tasks — corgi agent task add \"…\"")
			return
		}
		for _, col := range watch.TaskColumns {
			if !all && watch.FinishedState(col) != "" {
				continue
			}
			var rows []watch.Task
			for _, t := range tasks {
				if t.State == col {
					rows = append(rows, t)
				}
			}
			if len(rows) == 0 {
				continue
			}
			fmt.Println(col)
			for _, t := range rows {
				ws := ""
				if t.Workspace != "" {
					ws = "  " + t.Workspace
				}
				fmt.Printf("  %-8s %s%s\n", t.Ref(), t.Title, ws)
			}
		}
	},
}

var agentTaskMoveCmd = &cobra.Command{
	Use:   "move <TASK-N> <Todo|Doing|Review|Done|Canceled>",
	Short: "Put a task in a column",
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		moveTask(args[0], args[1])
	},
}

var agentTaskDoneCmd = &cobra.Command{
	Use:   "done <TASK-N>",
	Short: "Finish a task: it leaves the inbox, the card goes to Done",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		moveTask(args[0], "Done")
	},
}

var agentTaskEditCmd = &cobra.Command{
	Use:   "edit <TASK-N>",
	Short: "Change a task's title, description or workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		title, _ := cmd.Flags().GetString("title")
		body, _ := cmd.Flags().GetString("body")
		ws, _ := cmd.Flags().GetString("workspace")
		if ws = strings.TrimSpace(ws); ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				exitWithError("agent_task", fmt.Errorf("no such workspace: %s", ws), 2)
			}
		}
		t, err := watch.LoadTasks(dir).Edit(args[0], title, body, ws, time.Now())
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ref": t.Ref(), "task": t})
			return
		}
		fmt.Printf("%s: %s\n", t.Ref(), t.Title)
	},
}

var agentTaskRmCmd = &cobra.Command{
	Use:   "rm <TASK-N>",
	Short: "Take a task off the board for good",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		dir := mustAgentDir()
		t, err := watch.LoadTasks(dir).Remove(args[0])
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		_ = watch.LoadState(dir).Ignore(t.Key())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ref": t.Ref(), "removed": true})
			return
		}
		fmt.Printf("%s removed\n", t.Ref())
	},
}

func moveTask(arg, column string) {
	dir := mustAgentDir()
	t, err := watch.LoadTasks(dir).Move(arg, column, time.Now())
	if err != nil {
		exitWithError("agent_task", err, 2)
	}
	nudgeDaemon(dir)
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"ref": t.Ref(), "state": t.State})
		return
	}
	fmt.Printf("%s → %s\n", t.Ref(), t.State)
}

// nudgeDaemon asks a running daemon to publish, so every surface sees a
// moved task now rather than at the next poll. Quietly nothing without one.
func nudgeDaemon(dir string) {
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		return
	}
	daemon.Nudge(info)
}

// workspaceIDForDir is the registered workspace a directory is inside, "".
func workspaceIDForDir(agentD, cwd string) string {
	registry, err := workspace.Load(agentRegistryPath(agentD))
	if err != nil {
		return ""
	}
	for _, ws := range registry.Workspaces {
		if underRoot(cwd, ws.AbsPath) {
			return ws.ID
		}
	}
	return ""
}

func init() {
	agentTaskAddCmd.Flags().String("body", "", "the description: what to do and how you will know it is done (- reads stdin)")
	agentTaskAddCmd.Flags().String("workspace", "", "the workspace the task is for (default: the one this folder is in)")
	agentTaskEditCmd.Flags().String("title", "", "a new title")
	agentTaskEditCmd.Flags().String("body", "", "a new description")
	agentTaskEditCmd.Flags().String("workspace", "", "a new workspace")
	agentTaskListCmd.Flags().Bool("all", false, "finished ones too")
	agentTaskCmd.AddCommand(agentTaskAddCmd, agentTaskListCmd, agentTaskMoveCmd, agentTaskDoneCmd, agentTaskEditCmd, agentTaskRmCmd)
	agentCmd.AddCommand(agentTaskCmd)
}
