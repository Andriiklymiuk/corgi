package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
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

	s.AddTool(mcp.NewTool("corgi_workspaces",
		mcp.WithDescription(
			"List the corgi stacks registered on this machine, with their paths and whether each is reachable. "+
				"Read-only. Use this to find out what you can work on."),
	), jsonHandler(func(mcp.CallToolRequest) (any, error) {
		return mcpWorkspaces()
	}))

	s.AddTool(mcp.NewTool("corgi_workspace_resolve",
		mcp.WithDescription(
			"Resolve a human name like \"the todo app\" to one registered workspace. Read-only. "+
				"Returns either a single workspace or a candidate list — it never guesses, because picking the "+
				"wrong one means editing the wrong repository. Echo the resolved path back to the user before working."),
		mcp.WithString("query", mcp.Required(), mcp.Description("What the user called it")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWorkspaceResolve(r.GetString("query", ""))
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
			"Remove the worktrees a branch materialized. The branches and their commits are left alone."),
		composeOpt,
		mcp.WithString("branch", mcp.Required(), mcp.Description("Branch whose worktrees should be removed")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", agentDangerousBlockedMsg)
		}
		return mcpWorktreesRelease(r.GetString("composePath", ""), r.GetString("branch", ""))
	}))

	s.AddTool(mcp.NewTool("corgi_preview_start",
		mcp.WithDescription(
			"Open a public tunnel to a running service so the user can watch it on their phone while you edit. "+
				"Returns immediately with state \"starting\"; poll corgi_preview_state for the URL. "+
				"The stack must already be up (corgi_up). Refused for a workspace marked sensitive. "+
				"Prefer corgi_diff when the question is \"what changed\" — it needs no tunnel and survives bad signal."),
		composeOpt,
		serviceOpt,
		mcp.WithString("branch", mcp.Description("Branch being previewed, recorded for context")),
		mcp.WithString("provider", mcp.Description("Tunnel provider (cloudflared|ngrok|localtunnel)")),
		mcp.WithString("tunnelName", mcp.Description("Named tunnel; gives a stable URL that survives a restart")),
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
		mcp.WithString("branch", mcp.Description("Diff the worktrees of this branch instead of the main checkouts")),
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

func mcpWorkspaces() (any, error) {
	registry, path, err := agentRegistry()
	if err != nil {
		return nil, err
	}
	registry.Reconcile(dirHasComposeFile)
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
	registry.Reconcile(dirHasComposeFile)
	return workspace.Resolve(registry, query), nil
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

func mcpWorktreesRelease(composePath, branch string) (any, error) {
	_, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	removed, err := utils.ReleaseBranchWorktrees(dir, branch)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", utils.ErrExecFailed, err)
	}
	return map[string]any{"branch": branch, "removed": removed}, nil
}

func mcpDiff(composePath, base, branch string, includePatch bool) (any, error) {
	corgi, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}

	var set *utils.WorktreeSet
	if strings.TrimSpace(branch) != "" {
		// Diff what the agent has been editing, not the user's own checkout.
		set, err = utils.MaterializeBranchAcrossRepos(corgi, dir, branch, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", utils.ErrExecFailed, err)
		}
	}
	return utils.DiffStack(utils.ServiceDirs(corgi, set), base, includePatch), nil
}

func mcpPreviewStart(r mcp.CallToolRequest) (any, error) {
	composePath := r.GetString("composePath", "")
	corgi, dir, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	service := r.GetString("service", "")
	port, ok := servicePort(corgi, service)
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
		TunnelName:  r.GetString("tunnelName", ""),
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

// servicePort finds a service's declared port.
func servicePort(corgi *utils.CorgiCompose, name string) (int, bool) {
	for i := range corgi.Services {
		if corgi.Services[i].ServiceName == name {
			return corgi.Services[i].Port, true
		}
	}
	return 0, false
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
			if added := workspace.MergeLegacy(registry, entries, dirHasComposeFile); added > 0 {
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
