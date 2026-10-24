package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The idea pages as MCP tools, so a session (or anything speaking MCP)
// reads a spec, answers its comments, leaves its own, and builds from it.

func registerIdeaMCPTools(s *server.MCPServer) {
	slugOpt := mcp.WithString("slug", mcp.Required(), mcp.Description("The page's slug, as corgi idea list shows it"))

	s.AddTool(newCorgiTool("corgi_idea_list",
		mcp.WithDescription("Every idea page on this machine - specs, HLDs, RFCs, ideas - newest change first, with how many comments are open and how many wait on the agent. Read-only."),
	), jsonHandler(func(mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		return map[string]any{"ideas": ideaRows(dir, true)}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_read",
		mcp.WithDescription("One page in full: its markdown, its split into tickets (with where each stands), every comment with its replies and status, and the board tasks it made. Read-only. A comment's anchor quotes the words on the page it is about. Comments with status open by a person wait on the agent: answer them with corgi_idea_comment (replyTo) and amend the page with corgi_idea_edit when they ask for a change."),
		slugOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		id, err := idea.Load(dir, r.GetString("slug", ""))
		if err != nil {
			return nil, err
		}
		out := ideaDocument(dir, id, false, true)
		waiting := id.ForAgent()
		if waiting == nil {
			waiting = []idea.Comment{}
		}
		out["waiting"] = waiting
		return out, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_new",
		mcp.WithDescription("Write a new page from a sentence: an idea (something to build from nothing), a spec (a feature in an existing product), an hld (a high-level design) or an rfc (a proposal). The agent names it, tags it, writes the page, splits it into tickets and leaves proposals as comments. Takes a minute or more. Pass markdown to keep a page already written: it is kept as is and only named, tagged and split."),
		mcp.WithString("text", mcp.Required(), mcp.Description("What to write about, in a sentence or two")),
		mcp.WithString("type", mcp.Description("idea (default), spec, hld or rfc")),
		mcp.WithString("workspace", mcp.Description("Workspace id the page belongs to; the agent reads the repository to fit it")),
		mcp.WithString("markdown", mcp.Description("A page already written, kept verbatim")),
		mcp.WithBoolean("tasks", mcp.Description("Put the tickets on the board now (default true)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		req := ideaRequest{Text: strings.TrimSpace(r.GetString("text", "")), Type: r.GetString("type", ""), Workspace: strings.TrimSpace(r.GetString("workspace", "")), From: r.GetString("markdown", ""), By: "mcp", Model: ideaModel, Tasks: r.GetBool("tasks", true)}
		if req.Text == "" && strings.TrimSpace(req.From) == "" {
			return nil, fmt.Errorf("say what the page is about")
		}
		id, tasks, err := newIdea(dir, req, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]any{"idea": id.Meta, "tasks": tasks, "link": ideaLink(dir, id.Meta, false), "file": id.Dir + "/" + idea.DocName}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_comment",
		mcp.WithDescription("Leave a comment on a page, on the words quoted (quote must appear on the page verbatim) or on the page as a whole, or reply under a comment (replyTo). A reply as the agent answers a waiting comment: pass by \"agent\" and changed true when the page was amended for it, which marks it fixed; otherwise it is marked answered. Any other by is a person's comment, which the agent will answer."),
		slugOpt,
		mcp.WithString("text", mcp.Required(), mcp.Description("The comment")),
		mcp.WithString("quote", mcp.Description("The words on the page it is about")),
		mcp.WithString("by", mcp.Description("Who says it (default: mcp); \"agent\" for the agent's own reply")),
		mcp.WithNumber("replyTo", mcp.Description("Reply under this comment id instead of starting one")),
		mcp.WithBoolean("changed", mcp.Description("With by agent and replyTo: the page was changed for this comment")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		slug := r.GetString("slug", "")
		by := strings.TrimSpace(r.GetString("by", ""))
		if by == "" {
			by = "mcp"
		}
		replyTo := int(r.GetFloat("replyTo", 0))
		changed := r.GetBool("changed", false)
		var out any
		id, err := idea.Update(dir, slug, func(id *idea.Idea) error {
			now := time.Now()
			if replyTo > 0 {
				rp, err := id.Reply(replyTo, by, r.GetString("text", ""), changed && by == idea.Agent, nil, nil, now)
				out = rp
				return err
			}
			var anchor *idea.Anchor
			if q := strings.TrimSpace(r.GetString("quote", "")); q != "" {
				anchor = anchorFor(id.Doc, q)
				if anchor == nil {
					return fmt.Errorf("the quote is not on the page verbatim: %q", clipTitle(q, 80))
				}
			}
			c, err := id.AddComment(idea.KindComment, by, r.GetString("text", ""), anchor, nil, nil, now)
			out = c
			return err
		})
		if err != nil {
			return nil, err
		}
		if by != idea.Agent {
			ideaNotify(dir, id.Meta, "comment by "+by, clipTitle(r.GetString("text", ""), 160))
			scheduleIdeaAnswer(dir, slug)
		} else {
			ideaNotify(dir, id.Meta, "answered", clipTitle(r.GetString("text", ""), 160))
		}
		return map[string]any{"comment": out, "open": id.Open(), "forAgent": len(id.ForAgent())}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_resolve",
		mcp.WithDescription("Close a comment (resolved) or open it again (open)."),
		slugOpt,
		mcp.WithNumber("id", mcp.Required(), mcp.Description("The comment id")),
		mcp.WithString("status", mcp.Description("resolved (default) or open")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		status := r.GetString("status", idea.StatusResolved)
		if status != idea.StatusOpen && status != idea.StatusResolved {
			return nil, fmt.Errorf("status is resolved or open")
		}
		var c idea.Comment
		id, err := idea.Update(dir, r.GetString("slug", ""), func(id *idea.Idea) error {
			var err error
			c, err = id.SetStatus(int(r.GetFloat("id", 0)), status, time.Now())
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"comment": c, "open": id.Open()}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_edit",
		mcp.WithDescription("Replace the page's markdown (the whole page), or its split into tickets, or both. Use it after reading the page, when a comment asks for a change; then reply to the comment with corgi_idea_comment (by agent, changed true)."),
		slugOpt,
		mcp.WithString("markdown", mcp.Description("The whole page")),
		mcp.WithArray("tickets", mcp.Description("The split: [{title, body, after:[1-based numbers]}]"), mcp.Items(map[string]any{"type": "object"})),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		md := r.GetString("markdown", "")
		var split []plannerTask
		if raw, ok := r.GetArguments()["tickets"].([]any); ok {
			for _, item := range raw {
				m, _ := item.(map[string]any)
				title, _ := m["title"].(string)
				body, _ := m["body"].(string)
				if strings.TrimSpace(title) == "" {
					continue
				}
				t := plannerTask{Title: title, Body: body}
				if after, ok := m["after"].([]any); ok {
					for _, n := range after {
						if f, ok := n.(float64); ok {
							t.After = append(t.After, int(f))
						}
					}
				}
				split = append(split, t)
			}
		}
		if strings.TrimSpace(md) == "" && len(split) == 0 {
			return nil, fmt.Errorf("give markdown, tickets or both")
		}
		id, err := idea.Update(dir, r.GetString("slug", ""), func(id *idea.Idea) error {
			now := time.Now()
			if strings.TrimSpace(md) != "" {
				if err := id.SetDoc(strings.TrimSpace(md)+"\n", now); err != nil {
					return err
				}
			}
			if len(split) > 0 {
				id.Meta.Tickets = keepRefs(id.Meta.Tickets, ticketsFrom(split))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"idea": id.Meta, "tickets": id.Meta.Tickets}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_build",
		mcp.WithDescription("Hand the page to the agents: its tickets go on the board, a plan works through them, the daemon starts the first in a worktree. Needs a workspace (the page's, or given). Returns the plan, the tasks, whether it is running, and why not when it is not."),
		slugOpt,
		mcp.WithString("workspace", mcp.Description("Where it is built (default: the page's)")),
		mcp.WithNumber("slots", mcp.Description("How many tasks run at once (default 1)")),
		mcp.WithBoolean("run", mcp.Description("Start now (default true)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		return buildIdea(dir, r.GetString("slug", ""), r.GetString("workspace", ""), "", int(r.GetFloat("slots", 1)), r.GetBool("run", true))
	}))

	s.AddTool(newCorgiTool("corgi_idea_send",
		mcp.WithDescription("Send the page out: to Jira or Linear as an epic (a parent issue) with one ticket per line of the split, or to Confluence as a page. Uses the watch's credentials; the workspace's own tracker and project when none is given."),
		slugOpt,
		mcp.WithString("to", mcp.Required(), mcp.Description("jira, linear or confluence")),
		mcp.WithString("project", mcp.Description("Jira project key or Linear team key")),
		mcp.WithString("space", mcp.Description("Confluence space key")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		links, err := sendIdea(dir, r.GetString("slug", ""), r.GetString("to", ""), r.GetString("project", ""), r.GetString("space", ""))
		if err != nil {
			return nil, err
		}
		return map[string]any{"sent": links}, nil
	}))
}
