package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// What a workspace does on its own (corgi 2.22), for an agent client: read
// the switches, flip the ones that take at once, the mute, the fan-out's
// tries, the lessons. The same code the phone's switches go through.

func registerPolicyMCPTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("corgi_watch_switches",
		mcp.WithDescription("What each workspace does on its own, as switches: {workspace, enabled, action, prs, reviews, ci, comments, isolate, quiet, daysOff, autoMerge, handOver, autoAllow, doneWhen[], compactAt, rebase, lessons}. autoAllow \"reads\" means the daemon answers Read/Grep/Glob prompts itself; doneWhen are the commands that define finished (a red one is typed back into the session); compactAt sends /compact past that context fill; rebase rebases a stopped clean branch when main moved; lessons writes reviews, red checks and failed bots down for every new session. Read-only."),
		mcp.WithString("workspace", mcp.Description("Only this workspace id")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWatchSwitches(r.GetString("workspace", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_watch_set",
		mcp.WithDescription("Flip a workspace's own switches — autoAllow, doneWhen, compactAt, rebase, lessons, handOver, autoMerge — the ones that take on the daemon's next round with no restart. Do it when the user asks for the behaviour (\"answer the read prompts yourself\", \"a session is not done until go test passes\", \"rebase my branch when main moves\"), never to tidy up: each one makes the daemon act on their code. Omitted fields keep their value."),
		mcp.WithString("workspace", mcp.Required(), mcp.Description("Workspace id")),
		mcp.WithString("autoAllow", mcp.Description("reads, or off")),
		mcp.WithArray("doneWhen", mcp.Description("Commands run in the session's directory when it stops with changes; empty list turns it off"), mcp.WithStringItems()),
		mcp.WithNumber("compactAt", mcp.Description("Context percent past which a stopped session gets /compact; 0 is off")),
		mcp.WithBoolean("rebase", mcp.Description("Rebase a stopped session's clean branch onto main when main moved")),
		mcp.WithBoolean("lessons", mcp.Description("Write lessons down for every new session")),
		mcp.WithBoolean("handOver", mcp.Description("Type feedback on a branch into the session on it")),
		mcp.WithBoolean("autoCarry", mcp.Description("Carry a session at its five-hour quota to another of the workspace's accounts with budget, once per limit")),
		mcp.WithBoolean("rerunCI", mcp.Description("Rerun a red build's failed jobs once before it is worked on or handed over (GitHub)")),
		mcp.WithBoolean("headless", mcp.Description("Let a message for a session whose terminal is gone run as one headless turn (claude -p --resume)")),
		mcp.WithBoolean("autoMerge", mcp.Description("Merge a pull request of mine the moment it is ready")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWatchSet(r)
	}))

	s.AddTool(mcp.NewTool("corgi_agent_mute",
		mcp.WithDescription("Nothing rings for a while — no desktop toast, no phone push, no permission ping — while the inbox and the board go on. `for` is a duration up to 24h (1h, 30m) or off; omitted reads the current state. Returns {muted, until}."),
		mcp.WithString("for", mcp.Description("1h, 30m, off; omitted only reads")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpMute(r.GetString("for", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_agent_attempts",
		mcp.WithDescription("The sessions a fan-out opened on a ticket (corgi agent watch work <ref> --attempts N), side by side: [{ref, attempts[{n, session, label, status, model, branch, changes, tests, gate, spend, pr, summary, picked}]}]. With `pick` (the attempt's n) that one is kept: a note on it, the others interrupted and marked not picked, their worktrees left. Read-only without pick."),
		mcp.WithString("ref", mcp.Description("Only this ticket's tries")),
		mcp.WithString("pick", mcp.Description("Keep this attempt (its n) — needs ref")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpAttempts(r.GetString("ref", ""), r.GetString("pick", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_agent_lessons",
		mcp.WithDescription("What a workspace learned the hard way, one line each, oldest first: [{at, source, text}] — reviews on the user's pull requests, checks that stayed red, bots that failed, lines the user wrote. Read them before changing code in that workspace. With `add`, write one line yourself (say why in the line)."),
		mcp.WithString("workspace", mcp.Description("Workspace id; omitted means the one the cwd is in")),
		mcp.WithString("add", mcp.Description("A lesson to write, one line")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpLessons(r.GetString("workspace", ""), r.GetString("add", ""))
	}))
}

func mcpWatchSwitches(only string) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil {
		user = &config.UserConfig{}
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	list := []WatchSwitches{}
	for _, ws := range registry.Sorted() {
		if only != "" && ws.ID != only {
			continue
		}
		repo, _ := config.LoadRepo(ws.AbsPath)
		list = append(list, switchesOf(ws.ID, config.Resolve(ws.ID, repo, user).Watch))
	}
	return map[string]any{"workspaces": list}, nil
}

func mcpWatchSet(r mcp.CallToolRequest) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil || user == nil {
		user = &config.UserConfig{}
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	ws, ok := registry.Find(strings.TrimSpace(r.GetString("workspace", "")))
	if !ok {
		return nil, fmt.Errorf("no workspace named %q", r.GetString("workspace", ""))
	}
	if user.Workspaces == nil {
		user.Workspaces = map[string]config.WorkspaceConfig{}
	}
	entry := user.Workspaces[ws.ID]
	wc := entry.Watch
	if wc == nil {
		wc = &config.WatchConfig{}
	}
	args := r.GetArguments()
	if v, ok := args["autoAllow"].(string); ok {
		policy, err := config.ParseAutoAllow(v)
		if err != nil {
			return nil, err
		}
		wc.AutoAllow = policy
	}
	if v, ok := args["doneWhen"].([]any); ok {
		wc.DoneWhen = nil
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				wc.DoneWhen = append(wc.DoneWhen, strings.TrimSpace(s))
			}
		}
	}
	if v, ok := args["compactAt"].(float64); ok {
		if v < 0 || v > 100 {
			return nil, fmt.Errorf("compactAt is a percent, 0 to 100")
		}
		wc.CompactAt = int(v)
	}
	for key, dst := range map[string]*bool{"rebase": &wc.Rebase, "lessons": &wc.Lessons, "handOver": &wc.HandOver, "autoMerge": &wc.AutoMerge, "autoCarry": &wc.AutoCarry, "rerunCI": &wc.RerunCI, "headless": &wc.Headless} {
		if v, ok := args[key].(bool); ok {
			*dst = v
		}
	}
	entry.Watch = wc
	user.Workspaces[ws.ID] = entry
	if err := writeUserConfig(path, user); err != nil {
		return nil, err
	}
	return map[string]any{"done": "saved", "watch": switchesOf(ws.ID, wc), "takesEffect": "next round, no restart"}, nil
}

func mcpMute(word string) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	word = strings.ToLower(strings.TrimSpace(word))
	if word != "" {
		var until time.Time
		if word != "off" {
			d, err := time.ParseDuration(word)
			if err != nil || d <= 0 || d > 24*time.Hour {
				return nil, fmt.Errorf("for is a duration up to 24h (1h, 30m) or off, not %q", word)
			}
			until = time.Now().Add(d)
		}
		if err := daemon.SetMute(dir, until); err != nil {
			return nil, err
		}
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
			daemon.Nudge(info)
		}
	}
	until := daemon.MutedUntil(dir)
	out := map[string]any{"muted": !until.IsZero()}
	if !until.IsZero() {
		out["until"] = until
	}
	return out, nil
}

func mcpAttempts(ref, pick string) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	rep, err := readBoard(dir)
	if err != nil {
		return nil, err
	}
	groups := attemptGroups(rep.State.Sessions, ref)
	if pick == "" {
		if groups == nil {
			groups = []AttemptGroup{}
		}
		return map[string]any{"groups": groups}, nil
	}
	if ref == "" || len(groups) == 0 {
		return nil, fmt.Errorf("pick needs a ref with tries on the board")
	}
	found := false
	for _, a := range groups[0].Attempts {
		if a.N == pick {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("no attempt %s on %s", pick, groups[0].Ref)
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		return nil, fmt.Errorf("corgi agent is not running")
	}
	for _, c := range pickCommands(groups[0], pick) {
		c.Source = "mcp"
		if _, err := command.Write(dir, c); err != nil {
			return nil, err
		}
	}
	daemon.Nudge(info)
	return map[string]any{"picked": pick, "ref": groups[0].Ref}, nil
}

func mcpLessons(ws, add string) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if ws == "" {
		id, err := currentWorkspaceID(dir)
		if err != nil {
			return nil, err
		}
		ws = id
	}
	if strings.TrimSpace(add) != "" {
		if err := lessons.Add(dir, ws, lessons.Lesson{Source: "agent", Text: add}); err != nil {
			return nil, err
		}
	}
	list := lessons.List(dir, ws)
	if list == nil {
		list = []lessons.Lesson{}
	}
	return map[string]any{"workspace": ws, "path": lessons.Path(dir, ws), "lessons": list}, nil
}
