package cmd

import (
	"fmt"
	"os"
	"strings"

	"andriiklymiuk/corgi/utils"
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
