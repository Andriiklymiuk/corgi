package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type taskRow struct {
	Ref       string           `json:"ref"`
	Title     string           `json:"title"`
	State     string           `json:"state"`
	Workspace string           `json:"workspace,omitempty"`
	Owner     string           `json:"owner,omitempty"`
	Priority  string           `json:"priority,omitempty"`
	Due       string           `json:"due,omitempty"`
	Overdue   bool             `json:"overdue,omitempty"`
	WaitsFor  []string         `json:"waitsFor,omitempty"`
	Triage    bool             `json:"triage,omitempty"`
	Cycle     string           `json:"cycle,omitempty"`
	Idea      string           `json:"idea,omitempty"`
	Links     []watch.TaskLink `json:"links,omitempty"`
}

func taskRowOf(tasks *watch.TaskLog, t watch.Task, now time.Time) taskRow {
	return taskRow{Ref: t.Ref(), Title: t.Title, State: t.State, Workspace: t.Workspace, Owner: t.Owner, Priority: t.Priority, Due: t.Due,
		Overdue: t.Overdue(now), WaitsFor: tasks.OpenBlockers(t), Triage: t.Triage, Cycle: t.Cycle, Idea: t.Idea, Links: t.LinkRows()}
}

func stringsArg(r mcp.CallToolRequest, name string) ([]string, bool) {
	raw, ok := r.GetArguments()[name].([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out, true
}

func registerTaskMCPTools(s *server.MCPServer) {
	refOpt := mcp.WithString("ref", mcp.Required(), mcp.Description("The task, like TASK-3"))

	s.AddTool(newCorgiTool("corgi_task_list",
		mcp.WithDescription("The board's own tasks in the order they start: highest priority first, then nearest due date. Read-only. Each row says who holds it (owner), what it waits for (waitsFor), whether it still waits for the person's yes (triage) and its links to Jira, Linear, GitHub or GitLab. Before you start work on a task, check owner: a task another name holds is not yours to start. Finished tasks are left out unless all is true."),
		mcp.WithString("workspace", mcp.Description("Only this workspace's tasks")),
		mcp.WithBoolean("all", mcp.Description("Finished ones too")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		tasks, now, ws := watch.LoadTasks(dir), time.Now(), strings.TrimSpace(r.GetString("workspace", ""))
		rows := []taskRow{}
		for _, t := range tasks.ByPriority() {
			if (ws != "" && t.Workspace != ws) || (!r.GetBool("all", false) && watch.FinishedState(t.State) != "") {
				continue
			}
			rows = append(rows, taskRowOf(tasks, t, now))
		}
		return map[string]any{"tasks": rows}, nil
	}))

	s.AddTool(newCorgiTool("corgi_task_add",
		mcp.WithDescription("Put a task on the board. It waits in triage for the person's yes unless triage is false: use false only when the person asked for this exact task. Nothing starts on a task in triage. Say in the body what to do and how to know it is done."),
		mcp.WithString("title", mcp.Required(), mcp.Description("One line")),
		mcp.WithString("body", mcp.Description("What to do and how to know it is done")),
		mcp.WithString("workspace", mcp.Description("Workspace id")),
		mcp.WithBoolean("triage", mcp.Description("Wait for the person's yes (default true)")),
		mcp.WithArray("links", mcp.Description("http(s) links to a ticket or pull request"), mcp.Items(map[string]any{"type": "string"})),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		ws := strings.TrimSpace(r.GetString("workspace", ""))
		if ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				return nil, fmt.Errorf("no such workspace: %s", ws)
			}
		}
		tasks, now := watch.LoadTasks(dir), time.Now()
		var t watch.Task
		if r.GetBool("triage", true) {
			t, err = tasks.AddForTriage(r.GetString("title", ""), r.GetString("body", ""), ws, "mcp", now)
		} else {
			t, err = tasks.Add(r.GetString("title", ""), r.GetString("body", ""), ws, "mcp", now)
		}
		if err != nil {
			return nil, err
		}
		if links, ok := stringsArg(r, "links"); ok && len(links) > 0 {
			if t, err = tasks.Link(t.Ref(), links, now); err != nil {
				return nil, err
			}
		}
		return taskRowOf(tasks, t, now), nil
	}))

	s.AddTool(newCorgiTool("corgi_task_claim",
		mcp.WithDescription("Say you are taking a task, before you start on it, so no other agent or person starts the same one. It fails when another name holds the task: pick another task then, do not force it. Release it with corgi_task_release when you stop without finishing."),
		refOpt,
		mcp.WithString("as", mcp.Required(), mcp.Description("Who is taking it: your session's label or the agent's name")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		tasks, now := watch.LoadTasks(dir), time.Now()
		t, err := tasks.Claim(r.GetString("ref", ""), r.GetString("as", ""), false, now)
		if err != nil {
			return nil, err
		}
		return taskRowOf(tasks, t, now), nil
	}))

	s.AddTool(newCorgiTool("corgi_task_release",
		mcp.WithDescription("Give a task you hold back to the board. Only the name that holds it may release it."),
		refOpt,
		mcp.WithString("as", mcp.Required(), mcp.Description("The name you claimed it under")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		tasks, now := watch.LoadTasks(dir), time.Now()
		t, ok := tasks.Find(r.GetString("ref", ""))
		if !ok {
			return nil, fmt.Errorf("no task %s", r.GetString("ref", ""))
		}
		if !strings.EqualFold(t.Owner, strings.TrimSpace(r.GetString("as", ""))) {
			return nil, fmt.Errorf("%s is held by %s: only they release it", t.Ref(), firstNonEmptyString(t.Owner, "nobody"))
		}
		if t, err = tasks.Release(t.Ref(), now); err != nil {
			return nil, err
		}
		return taskRowOf(tasks, t, now), nil
	}))

	s.AddTool(newCorgiTool("corgi_task_update",
		mcp.WithDescription("Change a task's plan: its priority, its due date, the tasks it waits for, or add links to a ticket or pull request. Fields left out stay as they are. It does not move the task between columns and does not take it out of triage: that is the person's call."),
		refOpt,
		mcp.WithString("priority", mcp.Description("urgent, high, medium, low or none")),
		mcp.WithString("due", mcp.Description("A day like 2026-12-01, or none")),
		mcp.WithArray("blockedBy", mcp.Description("Tasks that must finish first, like [\"TASK-2\"]; an empty list clears it"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithArray("links", mcp.Description("http(s) links to add"), mcp.Items(map[string]any{"type": "string"})),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		tasks, now, ref := watch.LoadTasks(dir), time.Now(), r.GetString("ref", "")
		t, ok := tasks.Find(ref)
		if !ok {
			return nil, fmt.Errorf("no task %s", ref)
		}
		if _, given := r.GetArguments()["priority"]; given {
			if t, err = tasks.SetPriority(ref, r.GetString("priority", ""), now); err != nil {
				return nil, err
			}
		}
		if _, given := r.GetArguments()["due"]; given {
			if t, err = tasks.SetDue(ref, r.GetString("due", ""), now); err != nil {
				return nil, err
			}
		}
		if blockers, given := stringsArg(r, "blockedBy"); given {
			if t, err = tasks.SetBlockedBy(ref, blockers, now); err != nil {
				return nil, err
			}
		}
		if links, given := stringsArg(r, "links"); given && len(links) > 0 {
			if t, err = tasks.Link(ref, links, now); err != nil {
				return nil, err
			}
		}
		return taskRowOf(tasks, t, now), nil
	}))

	s.AddTool(newCorgiTool("corgi_task_note",
		mcp.WithDescription("Leave a note on a task as the agent: a decision you made alone and why, or what is left. An agent's note never starts a run. Keep it to what the next reader needs."),
		refOpt,
		mcp.WithString("text", mcp.Required(), mcp.Description("The note")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		tasks, now := watch.LoadTasks(dir), time.Now()
		t, err := tasks.Comment(r.GetString("ref", ""), watch.TaskAgent, r.GetString("text", ""), now)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ref": t.Ref(), "comments": len(t.Comments)}, nil
	}))
}
