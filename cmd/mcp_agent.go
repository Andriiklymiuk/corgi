package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// These are the tools a Remote Control session calls from a phone to do what
// Remote Control cannot: find the right stack among many, materialize a branch
// across every repository in it, and read the whole change at once.
//
// Two rules hold for all of them:
//
//   - Nothing blocks. cmd/mcp.go serializes every handler behind one global
//     mutex (handlers swap os.Stdout and mutate rootCmd flags), so a handler
//     that waited on slow work would freeze the server for every client.
//   - Anything that mutates joins the existing dangerous-tool tunnel gate, the
//     same one that already covers corgi_exec and corgi_db_query.

const agentDangerousBlockedMsg = "corgi_worktrees_* are disabled over a public tunnel; set CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1 to allow"

func registerAgentMCPTools(s *server.MCPServer) {
	composeOpt := mcp.WithString("composePath", mcp.Description("compose path (default: cwd)"))
	serviceOpt := mcp.WithString("service", mcp.Required(), mcp.Description("Service name"))

	s.AddTool(mcp.NewTool("corgi_agent_status",
		mcp.WithDescription(
			"Health of the corgi agent daemon: whether it is running, each workspace's supervised session, "+
				"restart count, wake lock, and which Claude account each workspace uses. Read-only. "+
				"Use this to answer \"is it up\" and \"why did my session die\"."),
	), jsonHandler(func(mcp.CallToolRequest) (any, error) {
		return mcpAgentStatus()
	}))

	s.AddTool(mcp.NewTool("corgi_session_brief",
		mcp.WithDescription(
			"What the previous supervised session in this workspace was working on before it was restarted. "+
				"Read-only. A restart produces a NEW session with none of the earlier conversation, so call this "+
				"first when the user picks up where they left off: it reports the branch each repository is on, "+
				"which hold uncommitted changes, and which cross-repo worktrees exist. "+
				"Returns null when nothing has restarted, which is the ordinary case."),
		mcp.WithString("workspace", mcp.Description("Workspace id; omit for every workspace that has one")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpSessionBrief(r.GetString("workspace", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_workspaces",
		mcp.WithDescription(
			"List the corgi stacks registered on this machine, with their paths and whether each is reachable. "+
				"Read-only. Use this to find out what you can work on."),
	), jsonHandler(func(mcp.CallToolRequest) (any, error) {
		return mcpWorkspaces()
	}))

	s.AddTool(mcp.NewTool("corgi_workspace_resolve",
		mcp.WithDescription(
			"Resolve a human name like \"the recipe app\" to one registered workspace. Read-only. "+
				"Returns either a single workspace or a candidate list — it never guesses, because picking the "+
				"wrong one means editing the wrong repository. Echo the resolved path back to the user before working."),
		mcp.WithString("query", mcp.Required(), mcp.Description("What the user called it")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWorkspaceResolve(r.GetString("query", ""))
	}))

	// corgi_session_start/_stop are NOT behind the dangerous-tunnel gate, for
	// the same reason corgi_up/corgi_down are not: they are the point of a
	// phone-driven endpoint. What bounds them instead:
	//   - the endpoint is per-device-token authenticated (pairing), and a lost
	//     device is revoked, not tolerated — treat its token as the machine key;
	//   - starts are registry-scoped: a caller picks an existing workspace, it
	//     cannot define what runs or where;
	//   - a workspace marked `sensitive` refuses remote start (remoteResolver);
	//   - the started session's conversation still needs the owner's claude.ai
	//     account — the sessionUrl reaches paired devices, so status.json is
	//     0600 and the URL is not otherwise logged.
	// A stolen token can still stop the owner's sessions (a nuisance, not a
	// breach); revocation is the answer, as with every tool the token reaches.
	s.AddTool(mcp.NewTool("corgi_session_start",
		mcp.WithDescription(
			"Start a supervised Claude Code Remote Control session in a registered workspace, by name. "+
				"Returns immediately with state \"starting\" — poll corgi_agent_status until the workspace reports "+
				"running and (best-effort) a sessionUrl; opening that URL joins the conversation. Idempotent: an "+
				"already-running workspace returns state \"running\" with its URL. The optional profile picks a "+
				"named entry from the trusted agent config (a different Claude account, e.g. \"work\")."),
		mcp.WithString("workspace", mcp.Required(), mcp.Description("Workspace id, alias, or human name")),
		mcp.WithString("profile", mcp.Description("Profile name from the agent config's profiles: section")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpSessionStart(r.GetString("workspace", ""), r.GetString("profile", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_session_stop",
		mcp.WithDescription(
			"Stop the supervised session in a workspace. Returns immediately with state \"stopping\"; "+
				"poll corgi_agent_status to confirm. Stopping a workspace that is not running is a clean no-op."),
		mcp.WithString("workspace", mcp.Required(), mcp.Description("Workspace id, alias, or human name")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpSessionStop(r.GetString("workspace", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_session_events",
		mcp.WithDescription(
			"A workspace's session timeline, newest first: starts, exits with their classified cause and reason, "+
				"disables, and captured claude.ai session links. Use it to answer why a session died or restarted. "+
				"It never contains session output."),
		mcp.WithString("workspace", mcp.Required(), mcp.Description("Workspace id, alias, or human name")),
		mcp.WithNumber("limit", mcp.Description("Max events to return, newest first (default 30, 0 = all kept)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpSessionEvents(r.GetString("workspace", ""), r.GetInt("limit", 30))
	}))

	s.AddTool(mcp.NewTool("corgi_worktrees_materialize",
		mcp.WithDescription(
			"Give every repository in the stack a git worktree on one shared branch, creating the branch off each "+
				"repo's HEAD when it does not exist yet. This is how a change spans several repositories at once. "+
				"Returns one entry per service with the directory to work in. Fast; does not build or start anything."),
		composeOpt,
		mcp.WithString("branch", mcp.Required(), mcp.Description("Branch to materialize, e.g. feature/referral-code")),
		mcp.WithString("services", mcp.Description("Comma-separated service names (default: every service)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpWorktreesMaterialize(
			r.GetString("composePath", ""),
			r.GetString("branch", ""),
			splitCSV(r.GetString("services", "")),
		)
	}))

	s.AddTool(mcp.NewTool("corgi_worktrees_release",
		mcp.WithDescription(
			"Remove the worktrees a branch materialized. Branches and commits are left alone, and a worktree "+
				"with uncommitted changes is kept and reported rather than discarded — pass force to remove it anyway."),
		composeOpt,
		mcp.WithString("branch", mcp.Required(), mcp.Description("Branch whose worktrees should be removed")),
		mcp.WithBoolean("force", mcp.Description("Remove even worktrees holding uncommitted changes")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpWorktreesRelease(r.GetString("composePath", ""), r.GetString("branch", ""), r.GetBool("force", false))
	}))

	s.AddTool(mcp.NewTool("corgi_preview_start",
		mcp.WithDescription(
			"Open a public tunnel to a running service so the user can watch it on their phone while you edit. "+
				"Returns immediately with state \"starting\"; poll corgi_preview_state for the URL. "+
				"The stack must already be up (corgi_up). Refused for a workspace marked sensitive. "+
				"A quick tunnel's URL changes if it restarts; declare a named tunnel in the service's tunnel: block for a stable one. "+
				"Prefer corgi_diff when the question is \"what changed\" — it needs no tunnel and survives bad signal."),
		composeOpt,
		serviceOpt,
		mcp.WithString("branch", mcp.Description("Branch being previewed, recorded for context")),
		mcp.WithString("provider", mcp.Description("Tunnel provider (cloudflared|ngrok|localtunnel)")),
		mcp.WithNumber("idleMinutes", mcp.Description("Tear down after this long unwatched (default 20)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpPreviewStart(r)
	}))

	s.AddTool(mcp.NewTool("corgi_preview_state",
		mcp.WithDescription(
			"State of one preview, or all of them. States: starting (no URL yet), ready, broken (the tunnel is up "+
				"but nothing answers on the port — usually a build in progress), stopped. "+
				"Tell the user which state it is in rather than handing over a URL that shows a stack trace."),
		composeOpt,
		mcp.WithString("id", mcp.Description("Preview id or service name; omit for all")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpPreviewState(r.GetString("composePath", ""), r.GetString("id", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_preview_freeze",
		mcp.WithDescription(
			"Pin a preview so idle reaping leaves it alone while the user is reading it. Set frozen=false to release."),
		composeOpt,
		mcp.WithString("id", mcp.Required(), mcp.Description("Preview id or service name")),
		mcp.WithBoolean("frozen", mcp.Description("Default true")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		// Freezing disables idle reaping, which is what would otherwise close a
		// forgotten public URL. Same gate as the rest.
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpPreviewFreeze(r.GetString("composePath", ""), r.GetString("id", ""), r.GetBool("frozen", true))
	}))

	s.AddTool(mcp.NewTool("corgi_preview_stop",
		mcp.WithDescription(
			"Tear a preview down. Do this when the user is finished — a forgotten preview is a public URL onto seeded data."),
		composeOpt,
		mcp.WithString("id", mcp.Required(), mcp.Description("Preview id or service name")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpPreviewStop(r.GetString("composePath", ""), r.GetString("id", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_diff",
		mcp.WithDescription(
			"Diff every repository in the stack against a base branch, in one response. Read-only, needs no running "+
				"stack and no tunnel, so it works on a bad connection. This is usually the best way to show someone "+
				"what changed. Large patches are truncated rather than dropped."),
		composeOpt,
		mcp.WithString("base", mcp.Description("Base branch to compare against (default: main)")),
		mcp.WithString("branch", mcp.Description("Diff the existing worktrees of this branch instead of the main checkouts. Does not create anything — run corgi_worktrees_materialize first.")),
		mcp.WithBoolean("includePatch", mcp.Description("Include the unified diff per file (default true)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDiff(
			r.GetString("composePath", ""),
			r.GetString("base", ""),
			r.GetString("branch", ""),
			r.GetBool("includePatch", true),
		)
	}))
}

// --- handlers ---

func mcpAgentStatus() (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	status, err := daemon.ReadStatus(dir)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return map[string]any{
			"running": false,
			"hint":    "start it with `corgi agent serve`, or `corgi agent install` to start at login",
		}, nil
	}
	return status, nil
}

func mcpSessionBrief(workspace string) (any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspace) != "" {
		b, readErr := brief.Read(dir, workspace)
		if readErr != nil {
			return nil, readErr
		}
		if b == nil {
			// Explicitly not an error: "nothing has restarted" is the good case,
			// and an error here would read as a fault to whoever is holding the
			// phone.
			return map[string]any{
				"brief": nil,
				"note":  "no restart recorded for this workspace since the daemon started",
			}, nil
		}
		return map[string]any{"brief": b, "summary": b.Summary()}, nil
	}
	briefs, err := brief.List(dir)
	if err != nil {
		return nil, err
	}
	if briefs == nil {
		// Never null: a client that iterates the field should not have to
		// special-case "no restarts yet", which is the ordinary state.
		briefs = []brief.Brief{}
	}
	return map[string]any{"briefs": briefs}, nil
}

func mcpWorkspaces() (any, error) {
	registry, path, err := agentRegistry()
	if err != nil {
		return nil, err
	}
	registry.Reconcile(dirIsWorkspace)
	_ = workspace.Save(path, registry)
	return map[string]any{"workspaces": registry.Sorted()}, nil
}

func mcpWorkspaceResolve(query string) (any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%s: query is required", utils.ErrUsage)
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return nil, err
	}
	registry.Reconcile(dirIsWorkspace)
	return workspace.Resolve(registry, query), nil
}

// resolveForSession maps a human name to one workspace, or returns the
// candidate list shaped exactly like corgi_workspace_resolve.
func resolveForSession(query string) (*workspace.Workspace, any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil, fmt.Errorf("%s: workspace is required", utils.ErrUsage)
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return nil, nil, err
	}
	registry.Reconcile(dirIsWorkspace)
	res := workspace.Resolve(registry, query)
	if !res.Resolved() {
		return nil, res, nil
	}
	return res.Workspace, nil, nil
}

func mcpSessionStart(query, profile string) (any, error) {
	w, ambiguous, err := resolveForSession(query)
	if err != nil {
		return nil, err
	}
	if ambiguous != nil {
		return ambiguous, nil
	}
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("the corgi agent daemon is not running — run `corgi agent serve` on the laptop, or `corgi agent install` to start at login")
	}
	if !info.Commands {
		return nil, fmt.Errorf("the running corgi agent predates remote session start — restart it (`corgi agent stop` then `corgi agent serve`) on the laptop")
	}
	// No status short-circuit: reading status.json here to answer "already
	// running" races a stop that has been requested but not yet taken effect,
	// which would report a dying session as running and enqueue nothing. The
	// daemon's Supervising() check is the authoritative idempotency guard, and
	// it orders a queued stop before this start by requestedAt, so enqueuing
	// unconditionally is both correct and simplest.
	c, err := command.Write(dir, command.Command{
		Action: command.ActionStart, WorkspaceID: w.ID, Profile: profile, Source: "mcp",
	})
	if err != nil {
		return nil, err
	}
	daemon.Nudge(info)
	return map[string]any{
		"workspaceId": w.ID,
		"state":       "starting",
		"commandId":   c.ID,
		"hint":        "poll corgi_agent_status until this workspace is running; its sessionUrl opens the conversation",
	}, nil
}

func mcpSessionStop(query string) (any, error) {
	w, ambiguous, err := resolveForSession(query)
	if err != nil {
		return nil, err
	}
	if ambiguous != nil {
		return ambiguous, nil
	}
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("the corgi agent daemon is not running — nothing to stop")
	}
	if !info.Commands {
		return nil, fmt.Errorf("the running corgi agent predates remote session start — restart it on the laptop")
	}
	c, err := command.Write(dir, command.Command{
		Action: command.ActionStop, WorkspaceID: w.ID, Source: "mcp",
	})
	if err != nil {
		return nil, err
	}
	daemon.Nudge(info)
	return map[string]any{
		"workspaceId": w.ID, "state": "stopping", "commandId": c.ID,
		"hint": "poll corgi_agent_status to confirm it stopped",
	}, nil
}

func mcpSessionEvents(query string, limit int) (any, error) {
	w, ambiguous, err := resolveForSession(query)
	if err != nil {
		return nil, err
	}
	if ambiguous != nil {
		return ambiguous, nil
	}
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"workspaceId": w.ID,
		"events":      events.NewLog(dir).Read(w.ID, limit),
	}, nil
}

func mcpWorktreesMaterialize(composePath, branch string, services []string) (any, error) {
	corgi, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	set, err := utils.MaterializeBranchAcrossRepos(corgi, dir, branch, services)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", utils.ErrExecFailed, err)
	}
	return set, nil
}

func mcpWorktreesRelease(composePath, branch string, force bool) (any, error) {
	_, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}

	var removed, keptDirty []string
	if force {
		removed, keptDirty, err = utils.ReleaseBranchWorktreesForce(dir, branch)
	} else {
		removed, keptDirty, err = utils.ReleaseBranchWorktreesReport(dir, branch)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", utils.ErrExecFailed, err)
	}

	out := map[string]any{"branch": branch, "removed": removed}
	if len(keptDirty) > 0 {
		out["keptUncommitted"] = keptDirty
		out["hint"] = "these hold uncommitted changes and were kept; pass force: true to remove them anyway"
	}
	return out, nil
}

func mcpDiff(composePath, base, branch string, includePatch bool) (any, error) {
	corgi, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}

	var set *utils.WorktreeSet
	if strings.TrimSpace(branch) != "" {
		// Look the worktrees up; never create them. This tool is advertised as
		// read-only and is deliberately ungated, so it must not be a way around
		// the gate on corgi_worktrees_materialize.
		set, err = utils.ExistingBranchWorktrees(corgi, dir, branch)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", utils.ErrUsage, err)
		}
		if len(set.Worktrees) == 0 {
			// Falling back to the main checkouts here would return someone
			// else's work labelled as this branch.
			return nil, fmt.Errorf(
				"%s: no worktrees exist for branch %q — run corgi_worktrees_materialize first, "+
					"or omit branch to diff the main checkouts",
				utils.ErrUsage, branch)
		}
		// Only the services actually on this branch. Materializing a subset
		// leaves the rest on their main checkouts, and including those would
		// show unrelated uncommitted work inside a diff labelled with a branch
		// that has nothing to do with it.
		return utils.DiffStack(utils.WorktreeDirs(set), base, includePatch), nil
	}
	return utils.DiffStack(utils.ServiceDirs(corgi, nil), base, includePatch), nil
}

func mcpPreviewStart(r mcp.CallToolRequest) (any, error) {
	composePath := r.GetString("composePath", "")
	corgi, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	service := r.GetString("service", "")
	port, named, ok := serviceTunnelInfo(corgi, service)
	if !ok {
		return nil, fmt.Errorf("%s: no service called %q in this stack", utils.ErrServiceNotFound, service)
	}

	// Reap first so a stale entry cannot masquerade as a live preview.
	_, _ = utils.ReapPreviews(dir, time.Now())

	return utils.StartPreview(utils.PreviewOptions{
		ComposeDir:  dir,
		Workspace:   corgi.Name,
		Service:     service,
		Branch:      r.GetString("branch", ""),
		Port:        port,
		Provider:    r.GetString("provider", ""),
		NamedTunnel: named,
		IdleMinutes: r.GetInt("idleMinutes", 0),
		Sensitive:   workspaceIsSensitive(dir),
	})
}

func mcpPreviewState(composePath, id string) (any, error) {
	_, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	reaped, _ := utils.ReapPreviews(dir, time.Now())
	if id != "" {
		p, perr := utils.PreviewStatus(dir, id)
		if perr != nil {
			return nil, perr
		}
		return p, nil
	}
	previews, lerr := utils.ListPreviews(dir)
	if lerr != nil {
		return nil, lerr
	}
	return map[string]any{"previews": previews, "reaped": reaped}, nil
}

func mcpPreviewFreeze(composePath, id string, frozen bool) (any, error) {
	_, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	return utils.FreezePreview(dir, id, frozen)
}

func mcpPreviewStop(composePath, id string) (any, error) {
	_, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	if err := utils.StopPreview(dir, id); err != nil {
		return nil, err
	}
	return map[string]any{"stopped": id}, nil
}

// serviceTunnelInfo returns a service's port and whether it declares a named
// tunnel, which is what makes the preview URL survive a restart.
func serviceTunnelInfo(corgi *utils.CorgiCompose, name string) (port int, named, found bool) {
	for i := range corgi.Services {
		svc := &corgi.Services[i]
		if svc.ServiceName != name {
			continue
		}
		return svc.Port, svc.Tunnel != nil && svc.Tunnel.Name != "", true
	}
	return 0, false, false
}

// workspaceIsSensitive reads the committed repo config. A workspace may
// restrict itself; that is the one thing the committed file is trusted for.
func workspaceIsSensitive(dir string) bool {
	repo, err := config.LoadRepo(dir)
	return err == nil && repo != nil && repo.Workspace.Sensitive
}

// --- shared helpers ---

// agentRegistry loads the workspace registry the CLI and MCP both read, so
// every surface agrees on what exists.
func agentRegistry() (*workspace.Registry, string, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, "", err
	}
	path := agentRegistryPath(dir)
	registry, err := workspace.Load(path)
	if err != nil {
		return nil, "", err
	}
	if len(registry.Workspaces) == 0 {
		if legacy, lerr := utils.ListExecPaths(); lerr == nil {
			entries := make([]workspace.LegacyEntry, 0, len(legacy))
			for _, e := range legacy {
				entries = append(entries, workspace.LegacyEntry{Name: e.Name, Description: e.Description, Path: e.Path})
			}
			if workspace.MergeLegacy(registry, entries, dirHasComposeFile) > 0 {
				_ = workspace.Save(path, registry)
			}
		}
	}
	return registry, path, nil
}

// loadComposeForAgent parses the compose file and returns it with the directory
// it lives in. The compose context is released before returning, so a stale
// --filename cannot leak into the next tool call.
func loadComposeForAgent(composePath string) (*utils.CorgiCompose, string, error) {
	ctx, err := loadComposeCtx(composePath)
	if err != nil {
		return nil, "", err
	}
	defer ctx.cleanup()

	dir := utils.CorgiComposePathDir
	if dir == "" {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			dir = cwd
		}
	}
	return ctx.corgi, dir, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
