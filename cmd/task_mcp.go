package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/rooms"
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

	s.AddTool(newCorgiTool("corgi_task_next",
		mcp.WithDescription("Take the next ready task to work on: it becomes yours for a while (a lease) and moves to Doing. Ready means in Todo, held by nobody, not in triage, and every task it waits for is finished; highest priority first. Answers task: null when nothing is ready. Renew with corgi_task_renew while you work; finish with corgi_task_finish. A lease you stop renewing runs out and the task goes to the next agent."),
		mcp.WithString("as", mcp.Required(), mcp.Description("Your name: the session's label or the agent's name")),
		mcp.WithArray("tags", mcp.Description("Only tasks with all of these tags"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithString("workspace", mcp.Description("Only this workspace's tasks")),
		mcp.WithString("lease", mcp.Description("How long it is yours, like 1h (default 1h, at most 24h)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		lease, ok := queueDuration(r.GetString("lease", ""), watch.MaxTaskLease)
		if !ok {
			return nil, fmt.Errorf("lease is a duration like 1h")
		}
		tags, _ := stringsArg(r, "tags")
		t, found, err := watch.TakeNextTask(dir, watch.NextQuery{Owner: r.GetString("as", ""), Tags: tags, Workspace: strings.TrimSpace(r.GetString("workspace", "")), Lease: lease}, time.Now())
		if err != nil || !found {
			return map[string]any{"task": nil}, err
		}
		nudgeDaemon(dir)
		return map[string]any{"task": taskRowOf(watch.LoadTasks(dir), t, time.Now()), "body": t.WithThread(), "leaseUntil": t.LeaseUntil}, nil
	}))

	s.AddTool(newCorgiTool("corgi_task_renew",
		mcp.WithDescription("Keep a task you took with corgi_task_next: its lease starts again from now. Only the name that holds it may renew it."),
		refOpt,
		mcp.WithString("as", mcp.Required(), mcp.Description("The name you took it under")),
		mcp.WithString("lease", mcp.Description("Like 1h (default 1h, at most 24h)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		lease, ok := queueDuration(r.GetString("lease", ""), watch.MaxTaskLease)
		if !ok {
			return nil, fmt.Errorf("lease is a duration like 1h")
		}
		t, err := watch.RenewTask(dir, r.GetString("ref", ""), r.GetString("as", ""), lease, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]any{"ref": t.Ref(), "leaseUntil": t.LeaseUntil}, nil
	}))

	s.AddTool(newCorgiTool("corgi_task_finish",
		mcp.WithDescription("Finish a task you hold: it goes to Done with your result as its last note, like what changed and the pull request to review. Only the name that holds it may finish it."),
		refOpt,
		mcp.WithString("as", mcp.Required(), mcp.Description("The name you took it under")),
		mcp.WithString("result", mcp.Required(), mcp.Description("What was done, and the link to review")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		ref, who := r.GetString("ref", ""), strings.TrimSpace(r.GetString("as", ""))
		if t, ok := watch.LoadTasks(dir).Find(ref); !ok {
			return nil, fmt.Errorf("no task %s", ref)
		} else if !strings.EqualFold(t.Owner, who) {
			return nil, fmt.Errorf("%s is held by %s: only they finish it", t.Ref(), firstNonEmptyString(t.Owner, "nobody"))
		}
		t, err := watch.FinishTask(dir, ref, watch.TaskAgent, r.GetString("result", ""), time.Now())
		if err != nil {
			return nil, err
		}
		nudgeDaemon(dir)
		return map[string]any{"ref": t.Ref(), "state": t.State}, nil
	}))

	s.AddTool(newCorgiTool("corgi_room_post",
		mcp.WithDescription("Leave a message in a room for other agents or people: a finding, a question, a handover. Rooms are named like paths (security/findings). thread replies under a message id. With shape, data is checked against that saved shape and refused when it does not fit."),
		mcp.WithString("room", mcp.Required(), mcp.Description("Like security/findings")),
		mcp.WithString("as", mcp.Required(), mcp.Description("Your name")),
		mcp.WithString("text", mcp.Description("The words")),
		mcp.WithString("thread", mcp.Description("Reply under this message id, like m12")),
		mcp.WithString("shape", mcp.Description("Check data against this shape, like finding@1")),
		mcp.WithObject("data", mcp.Description("JSON to keep with the message")),
		mcp.WithObject("labels", mcp.Description("key: value pairs to filter by later, like {\"kind\": \"bug\"}")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		args := r.GetArguments()
		var data json.RawMessage
		if v, ok := args["data"]; ok && v != nil {
			if data, err = json.Marshal(v); err != nil {
				return nil, err
			}
		}
		labels := map[string]string{}
		if raw, ok := args["labels"].(map[string]any); ok {
			for k, v := range raw {
				labels[k] = fmt.Sprint(v)
			}
		}
		return rooms.Post(dir, rooms.Message{Room: r.GetString("room", ""), By: r.GetString("as", ""), Text: r.GetString("text", ""),
			Thread: r.GetString("thread", ""), Shape: r.GetString("shape", ""), Data: data, Labels: labels}, time.Now())
	}))

	s.AddTool(newCorgiTool("corgi_room_read",
		mcp.WithDescription("Read a room: the newest messages, or only those after a message id you already saw, or one thread. Without room it lists the rooms. Read-only."),
		mcp.WithString("room", mcp.Description("Like security/findings; leave out for the list of rooms")),
		mcp.WithString("after", mcp.Description("Only messages after this id, like m12")),
		mcp.WithString("thread", mcp.Description("Only this thread")),
		mcp.WithNumber("max", mcp.Description("At most this many (default 50)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(r.GetString("room", "")) == "" {
			return map[string]any{"rooms": rooms.List(dir)}, nil
		}
		list, err := rooms.Read(dir, rooms.Query{Room: r.GetString("room", ""), After: r.GetString("after", ""), Thread: r.GetString("thread", ""), Limit: r.GetInt("max", 50)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"messages": list}, nil
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
