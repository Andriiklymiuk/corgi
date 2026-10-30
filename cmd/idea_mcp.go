package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The idea pages as MCP tools, so a session (or anything speaking MCP)
// reads a spec, answers its comments, leaves its own, and builds from it.

func registerIdeaMCPTools(s *server.MCPServer) {
	slugOpt := mcp.WithString("slug", mcp.Required(), mcp.Description("The page's slug, as corgi idea list shows it (read and comment also take a link someone shared)"))

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
		mcp.WithDescription("One page in full: its markdown, its split into tickets (with where each stands), every comment with its replies and status, and the board tasks it made. Read-only. A comment's anchor quotes the words on the page it is about. Comments with status open by a person wait on the agent: answer them with corgi_idea_comment (replyTo) and amend the page with corgi_idea_edit when they ask for a change. A page someone shared with you: pass its link (https://…/idea/<slug>?k=…) as slug; you read it as a guest and comment as one."),
		slugOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if link, ok := parseIdeaLink(r.GetString("slug", "")); ok {
			data, err := link.do(http.MethodGet, "doc", nil)
			if err != nil {
				return nil, err
			}
			var out map[string]any
			return out, json.Unmarshal(data, &out)
		}
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
		mcp.WithDescription("Leave a comment on a page, on the words quoted (quote must appear on the page verbatim) or on the page as a whole, or reply under a comment (replyTo). A reply as the agent answers a waiting comment: pass by \"agent\" and changed true when the page was amended for it, which marks it fixed; otherwise it is marked answered. Any other by is a person's comment, which the agent will answer. On a page someone shared with you, pass its link as slug and your own name as by: the page's agent answers there."),
		slugOpt,
		mcp.WithString("text", mcp.Required(), mcp.Description("The comment")),
		mcp.WithString("quote", mcp.Description("The words on the page it is about")),
		mcp.WithString("prefix", mcp.Description("What comes just before the words, when they are on the page more than once")),
		mcp.WithString("suffix", mcp.Description("What comes just after them")),
		mcp.WithString("heading", mcp.Description("The section they are under")),
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
		if link, ok := parseIdeaLink(slug); ok {
			if by == idea.Agent {
				return nil, fmt.Errorf("on a page someone shared, comment under your own name, not as its agent")
			}
			return remoteIdeaComment(link, remoteComment{Text: r.GetString("text", ""), By: by, Quote: r.GetString("quote", ""), Prefix: r.GetString("prefix", ""), Suffix: r.GetString("suffix", ""), Heading: r.GetString("heading", ""), ReplyTo: replyTo})
		}
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
				anchor = anchorAt(id.Doc, q, r.GetString("prefix", ""), r.GetString("suffix", ""))
				if anchor == nil {
					return fmt.Errorf("the quote is not on the page verbatim: %q", clipTitle(q, 80))
				}
				if h := strings.TrimSpace(r.GetString("heading", "")); h != "" && anchor.Heading == "" {
					anchor.Heading = h
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

	s.AddTool(newCorgiTool("corgi_idea_decide",
		mcp.WithDescription("Write a page's decision: one plain line saying what was settled and why (\"Apple Pay ships first: most returning shoppers are on iPhone\"). Resolving a thread already leaves a rough line; give thread to reword that one, or leave it out to add a decision of your own. drop removes the decision at that place in the list."),
		slugOpt,
		mcp.WithString("text", mcp.Description("The decision, one sentence")),
		mcp.WithNumber("thread", mcp.Description("The comment id the decision settles")),
		mcp.WithNumber("drop", mcp.Description("Remove the decision at this 1-based place instead")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		id, err := idea.Update(dir, r.GetString("slug", ""), func(id *idea.Idea) error {
			if n := int(r.GetFloat("drop", 0)); n > 0 {
				return id.Undecide(n)
			}
			_, err := id.Decide(r.GetString("text", ""), idea.Agent, int(r.GetFloat("thread", 0)), time.Now())
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"decisions": id.Meta.Decisions}, nil
	}))

	s.AddTool(newCorgiTool("corgi_idea_mock",
		mcp.WithDescription("Have the agent draw the page's screens as one clickable HTML mockup, shown under the page in the browser (sandboxed). Runs in the background for a minute or two; comments on the mockup go into the next drawing. Returns at once."),
		slugOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		out, _, err := ideaPerform(dir, ideaAction{Do: "mock", Slug: r.GetString("slug", "")}, true, idea.Agent)
		return out, err
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
			id.ChangeBy, id.ChangeNote = idea.Agent, "edited by a session"
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

	s.AddTool(newCorgiTool("corgi_idea_set",
		mcp.WithDescription("Change what a page is called, its summary, tags, type, the workspace it belongs to and builds in, and whether the agent answers comments on its own. Fields left out stay as they are."),
		slugOpt,
		mcp.WithString("name", mcp.Description("The project's name")),
		mcp.WithString("summary", mcp.Description("The one-line summary")),
		mcp.WithArray("tags", mcp.Description("The tags (replaces them)"), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithString("workspace", mcp.Description("Workspace id")),
		mcp.WithString("type", mcp.Description("idea, spec, hld or rfc, or a team's own type from .corgi/idea-templates/")),
		mcp.WithBoolean("auto", mcp.Description("Whether the agent answers on its own")),
		mcp.WithNumber("approvals", mcp.Description("How many sign-offs make the page approved (1-20)")),
		mcp.WithBoolean("buildWhenApproved", mcp.Description("Hand the page to the agents the moment it is approved")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		ch := ideaChanges{Name: r.GetString("name", ""), Summary: r.GetString("summary", ""), Workspace: r.GetString("workspace", ""), Type: r.GetString("type", "")}
		if raw, ok := r.GetArguments()["auto"].(bool); ok {
			ch.Auto = map[bool]string{true: "on", false: "off"}[raw]
		}
		if raw, ok := r.GetArguments()["buildWhenApproved"].(bool); ok {
			ch.BuildWhenApproved = map[bool]string{true: "on", false: "off"}[raw]
		}
		if raw, ok := r.GetArguments()["approvals"].(float64); ok {
			n := int(raw)
			ch.Needed = &n
		}
		if raw, ok := r.GetArguments()["tags"].([]any); ok {
			var tags []string
			for _, t := range raw {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
			ch.Tags = &tags
		}
		id, err := setIdea(dir, r.GetString("slug", ""), ch)
		if err != nil {
			return nil, err
		}
		return map[string]any{"idea": id.Meta}, nil
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

	s.AddTool(newCorgiTool("corgi_idea_share",
		mcp.WithDescription("The link for the team: anyone with it reads the page, highlights words and comments; each comment reaches the agent, which answers and amends the page. Give slug for a page already here, or markdown (and title) to make a new spec page from text kept word for word. off closes the link, rotate gives a new one. Returns link and public (false: the link works on this machine only until corgi agent up gives it a public address)."),
		mcp.WithString("slug", mcp.Description("A page already here")),
		mcp.WithString("markdown", mcp.Description("Or the page's text, kept word for word")),
		mcp.WithString("title", mcp.Description("With markdown: the page's name (default: its first heading)")),
		mcp.WithString("type", mcp.Description("With markdown: spec (default), idea, hld or rfc")),
		mcp.WithString("workspace", mcp.Description("With markdown: the workspace it belongs to")),
		mcp.WithBoolean("off", mcp.Description("Close the link")),
		mcp.WithBoolean("rotate", mcp.Description("A new link; the old one stops working")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		dir, err := agentDir()
		if err != nil {
			return nil, err
		}
		slug, md := strings.TrimSpace(r.GetString("slug", "")), r.GetString("markdown", "")
		if slug == "" && strings.TrimSpace(md) == "" {
			return nil, fmt.Errorf("give slug or markdown")
		}
		if md == "" {
			if _, err := idea.Load(dir, slug); err != nil {
				return nil, err
			}
		}
		return shareIdea(dir, shareRequest{Target: slug, Markdown: md, Title: r.GetString("title", ""), Type: r.GetString("type", ""),
			Workspace: strings.TrimSpace(r.GetString("workspace", "")), By: "mcp", Off: r.GetBool("off", false), Rotate: r.GetBool("rotate", false)}, time.Now())
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
