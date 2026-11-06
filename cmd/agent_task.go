package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

var agentTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Tasks of your own on the board: add one for later, pick it up, move it along",
	Long: `A task is a ticket you write yourself - a title, a description, the
workspace it is for - kept on this machine and shown on the same board as the
tracker's tickets: the inbox, the Board tab, the page, the menu bar. It goes
through Todo, Doing, Review, Done (or Canceled), and nothing about it reaches
a tracker.

  corgi agent task add "Meta SDK on iOS" --workspace app --body "App Events, not the pixel"
  corgi agent task list
  corgi agent watch work TASK-3           a session on it: the description is the prompt
  corgi agent task move TASK-3 Review     the session runs this when a draft PR is up
  corgi agent task done TASK-3
  corgi agent task add "Fix the export" --workspace app --run     starts by itself, at level 3 and up
  corgi agent task comment TASK-3 "the total is still wrong"      a run picks it up
  corgi agent task add "Fix it" --workspace app --on build-1      on a peer: a server or another laptop

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
		run, _ := cmd.Flags().GetBool("run")
		on, _ := cmd.Flags().GetString("on")
		if body == "-" {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				exitWithError("agent_task", err, 1)
			}
			body = string(raw)
		}
		title := strings.Join(args, " ")
		if on = strings.TrimSpace(on); on != "" {
			ref, err := addTaskOnPeer(context.Background(), dir, on, title, body, strings.TrimSpace(ws), run)
			if err != nil {
				exitWithError("agent_task", err, 1)
			}
			fmt.Printf("%s on %s\n", ref, on)
			return
		}
		if ws = strings.TrimSpace(ws); ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				exitWithError("agent_task", fmt.Errorf("no such workspace: %s", ws), 2)
			}
		} else if cwd, err := os.Getwd(); err == nil {
			ws = workspaceIDForDir(dir, cwd)
		}
		if run {
			if why := taskRunRefusal(dir, ws); why != "" {
				exitWithError("agent_task", fmt.Errorf("%s", why), 2)
			}
		}
		t, err := watch.LoadTasks(dir).Add(title, body, ws, "cli", time.Now())
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		if run {
			t, _ = watch.LoadTasks(dir).AskRun(t.Ref(), time.Now())
			nudgePlans(dir)
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
			fmt.Println("no tasks - corgi agent task add \"…\"")
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

func nudgeDaemon(dir string) {
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		return
	}
	daemon.Nudge(info)
}

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

// addTaskOnPeer puts a task on another machine's board: the workspace is there,
// and so is the run. The peer checks the workspace and whether it can run.
func addTaskOnPeer(ctx context.Context, dir, name, title, body, ws string, run bool) (string, error) {
	return sendTaskToPeer(ctx, dir, name, title, body, ws, run, nil)
}

func sendTaskToPeer(ctx context.Context, dir, name, title, body, ws string, run bool, comments []watch.TaskComment) (string, error) {
	store, err := peers.Load(peers.Path(dir))
	if err != nil {
		return "", err
	}
	peer, ok := store.Find(name)
	if !ok {
		return "", fmt.Errorf("no peer %q: corgi agent peers lists them, corgi agent peers join adds one", name)
	}
	if ws == "" {
		return "", fmt.Errorf("--on needs --workspace: the name it has on %s", name)
	}
	var out struct {
		Ref string `json:"ref"`
	}
	client := peers.NewClient(peer)
	client.HTTP = &http.Client{Timeout: 30 * time.Second}
	err = client.Call(ctx, "/launch/task", map[string]any{"title": title, "body": body, "workspace": ws, "run": run, "comments": comments}, &out)
	return out.Ref, err
}

// taskSendRefusal says why this task cannot leave: it is over, or a run has it.
func taskSendRefusal(dir string, t watch.Task) string {
	switch {
	case t.State == "Done" || t.State == "Canceled":
		return t.Ref() + " is " + t.State + ": nothing to send"
	case t.State == "Doing":
		return t.Ref() + " is running here: send it when the run is over"
	case t.MovedTo != "":
		return t.Ref() + " already moved to " + t.MovedTo
	case t.Workspace == "":
		return t.Ref() + " names no workspace: corgi agent task edit it first"
	}
	for _, p := range watch.LoadPlans(dir).All() {
		for _, id := range p.TaskIDs() {
			if id == t.ID {
				return t.Ref() + " is part of " + p.Ref() + ": a plan runs on one machine"
			}
		}
	}
	return ""
}

var agentTaskSendCmd = &cobra.Command{
	Use:   "send <TASK-n> --to <peer>",
	Short: "Move a task to another machine, thread and all",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		to, _ := cmd.Flags().GetString("to")
		run, _ := cmd.Flags().GetBool("run")
		tasks := watch.LoadTasks(dir)
		t, ok := tasks.Find(args[0])
		if !ok {
			exitWithError("agent_task", fmt.Errorf("no task %s", args[0]), 2)
		}
		if why := taskSendRefusal(dir, t); why != "" {
			exitWithError("agent_task", fmt.Errorf("%s", why), 2)
		}
		ref, err := sendTaskToPeer(context.Background(), dir, to, t.Title, t.Body, t.Workspace, run || t.WantsRun(), t.Comments)
		if err != nil {
			exitWithError("agent_task", err, 1)
		}
		_, _ = tasks.MoveAway(t.ID, t.CreatedAt, to+" as "+ref, time.Now())
		nudgePlans(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"from": t.Ref(), "to": to, "ref": ref})
			return
		}
		fmt.Printf("%s moved to %s as %s\n", t.Ref(), to, ref)
	},
}

var agentTaskCommentCmd = &cobra.Command{
	Use:   "comment <TASK-n> <text>",
	Short: "Say something on a task; where the workspace runs fixes, a run picks it up",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		by, _ := cmd.Flags().GetString("by")
		t, err := watch.LoadTasks(dir).Comment(args[0], by, strings.Join(args[1:], " "), time.Now())
		if err != nil {
			exitWithError("agent_task", err, 2)
		}
		nudgePlans(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ref": t.Ref(), "comments": t.Comments})
			return
		}
		fmt.Printf("%s: %d comment(s)\n", t.Ref(), len(t.Comments))
	},
}

func init() {
	agentTaskCommentCmd.Flags().String("by", "you", "who is saying it")
	agentTaskSendCmd.Flags().String("to", "", "the peer to move it to (corgi agent peers lists them)")
	agentTaskSendCmd.Flags().Bool("run", false, "start it there, unattended, even if it was not waiting for a run")
	_ = agentTaskSendCmd.MarkFlagRequired("to")
	agentTaskAddCmd.Flags().Bool("run", false, "start it now, unattended, where the workspace runs fixes (corgi agent level 3)")
	agentTaskAddCmd.Flags().String("on", "", "put it on this peer's board: another laptop or a server (corgi agent peers)")
	agentTaskAddCmd.Flags().String("body", "", "the description: what to do and how you will know it is done (- reads stdin)")
	agentTaskAddCmd.Flags().String("workspace", "", "the workspace the task is for (default: the one this folder is in)")
	agentTaskEditCmd.Flags().String("title", "", "a new title")
	agentTaskEditCmd.Flags().String("body", "", "a new description")
	agentTaskEditCmd.Flags().String("workspace", "", "a new workspace")
	agentTaskListCmd.Flags().Bool("all", false, "finished ones too")
	agentTaskCmd.AddCommand(agentTaskCommentCmd, agentTaskSendCmd, agentTaskAddCmd, agentTaskListCmd, agentTaskMoveCmd, agentTaskDoneCmd, agentTaskEditCmd, agentTaskRmCmd)
	agentCmd.AddCommand(agentTaskCmd)
}
