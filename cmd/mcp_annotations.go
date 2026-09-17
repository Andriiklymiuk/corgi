package cmd

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// toolKind is what a tool does to state, which is what a client uses to
// decide whether to ask the person before running it. mcp-go's defaults mark
// every tool destructive, so an unlisted tool would make Claude ask before
// reading a status; newCorgiTool refuses to register one.
type toolKind int

const (
	toolReadOnly toolKind = iota
	toolWrite
	toolDestructive
)

type toolMeta struct {
	Title     string
	Kind      toolKind
	OpenWorld bool
}

var mcpToolMeta = map[string]toolMeta{
	"corgi_agent_attempts":        {Title: "Fan-out attempts on a ticket", Kind: toolWrite},
	"corgi_agent_lessons":         {Title: "Workspace lessons", Kind: toolWrite},
	"corgi_agent_mute":            {Title: "Mute notifications", Kind: toolWrite},
	"corgi_agent_status":          {Title: "Agent sessions status", Kind: toolReadOnly},
	"corgi_checkout":              {Title: "Check out a branch in every repo", Kind: toolDestructive},
	"corgi_checkpoint":            {Title: "Save a cross-repo checkpoint", Kind: toolWrite},
	"corgi_context":               {Title: "Stack overview", Kind: toolReadOnly},
	"corgi_db_query":              {Title: "Run SQL against a database", Kind: toolDestructive},
	"corgi_db_restore":            {Title: "Restore a database snapshot", Kind: toolDestructive},
	"corgi_db_snapshot":           {Title: "Snapshot a database", Kind: toolWrite},
	"corgi_diff":                  {Title: "Cross-repo diff", Kind: toolReadOnly},
	"corgi_doctor":                {Title: "Preflight checks", Kind: toolReadOnly},
	"corgi_down":                  {Title: "Stop the stack", Kind: toolDestructive},
	"corgi_env":                   {Title: "Resolved service environment", Kind: toolReadOnly},
	"corgi_exec":                  {Title: "Run a command in a service", Kind: toolDestructive},
	"corgi_explain":               {Title: "Explain a query plan", Kind: toolReadOnly},
	"corgi_http":                  {Title: "HTTP request to a service", Kind: toolWrite, OpenWorld: true},
	"corgi_logs":                  {Title: "Service logs", Kind: toolReadOnly},
	"corgi_plan":                  {Title: "Dry-run start plan", Kind: toolReadOnly},
	"corgi_pr_open":               {Title: "Open a pull request", Kind: toolDestructive},
	"corgi_preview_freeze":        {Title: "Freeze a preview", Kind: toolDestructive},
	"corgi_preview_start":         {Title: "Start a public preview", Kind: toolDestructive},
	"corgi_preview_state":         {Title: "Preview state", Kind: toolReadOnly},
	"corgi_preview_stop":          {Title: "Stop a preview", Kind: toolDestructive},
	"corgi_ps":                    {Title: "Running processes", Kind: toolReadOnly},
	"corgi_restart":               {Title: "Restart the stack", Kind: toolWrite},
	"corgi_restore":               {Title: "Restore a checkpoint", Kind: toolDestructive},
	"corgi_schema":                {Title: "Compose file schema", Kind: toolReadOnly},
	"corgi_session_brief":         {Title: "Session brief", Kind: toolReadOnly},
	"corgi_session_events":        {Title: "Session events", Kind: toolReadOnly},
	"corgi_session_start":         {Title: "Start an agent session", Kind: toolWrite},
	"corgi_session_stop":          {Title: "Stop an agent session", Kind: toolDestructive},
	"corgi_sessions":              {Title: "List agent sessions", Kind: toolReadOnly},
	"corgi_status":                {Title: "Stack health", Kind: toolReadOnly},
	"corgi_test":                  {Title: "Run service tests", Kind: toolWrite},
	"corgi_today":                 {Title: "Today's activity", Kind: toolReadOnly},
	"corgi_up":                    {Title: "Start the stack", Kind: toolWrite},
	"corgi_validate":              {Title: "Validate the compose file", Kind: toolReadOnly},
	"corgi_wait_for_log":          {Title: "Wait for a log line", Kind: toolReadOnly},
	"corgi_watch_board":           {Title: "Tracker board columns", Kind: toolReadOnly},
	"corgi_watch_enable":          {Title: "Enable the tracker watch", Kind: toolWrite},
	"corgi_watch_events":          {Title: "Watch events", Kind: toolReadOnly},
	"corgi_watch_fixes":           {Title: "Unattended fixes", Kind: toolReadOnly},
	"corgi_watch_move":            {Title: "Move a ticket", Kind: toolWrite},
	"corgi_watch_set":             {Title: "Set workspace switches", Kind: toolWrite},
	"corgi_watch_status":          {Title: "Watch status", Kind: toolReadOnly},
	"corgi_watch_switches":        {Title: "Workspace switches", Kind: toolReadOnly},
	"corgi_why":                   {Title: "Why a service is down", Kind: toolReadOnly},
	"corgi_workspace_resolve":     {Title: "Resolve a workspace by name", Kind: toolReadOnly},
	"corgi_workspaces":            {Title: "List workspaces", Kind: toolReadOnly},
	"corgi_worktrees_materialize": {Title: "Create worktrees", Kind: toolDestructive},
	"corgi_worktrees_release":     {Title: "Remove worktrees", Kind: toolDestructive},
}

func annotationOptions(m toolMeta) []mcp.ToolOption {
	opts := []mcp.ToolOption{
		mcp.WithTitleAnnotation(m.Title),
		mcp.WithOpenWorldHintAnnotation(m.OpenWorld),
	}
	switch m.Kind {
	case toolReadOnly:
		opts = append(opts,
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true))
	case toolWrite:
		opts = append(opts,
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false))
	case toolDestructive:
		opts = append(opts,
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false))
	}
	return opts
}

// newCorgiTool is mcp.NewTool plus the title and hints from mcpToolMeta.
func newCorgiTool(name string, opts ...mcp.ToolOption) mcp.Tool {
	m, ok := mcpToolMeta[name]
	if !ok {
		panic(fmt.Sprintf("mcp tool %q has no entry in mcpToolMeta; add a title and a kind", name))
	}
	return mcp.NewTool(name, append(opts, annotationOptions(m)...)...)
}
