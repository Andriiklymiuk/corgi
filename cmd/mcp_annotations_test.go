package cmd

import (
	"context"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// tunnelGatedDestructive are the tunnel-gated tools that change state; the
// two other gated tools (corgi_http, corgi_explain) are gated for
// reachability, not for what they do.
var tunnelGatedDestructive = []string{
	"corgi_exec", "corgi_db_query", "corgi_db_restore", "corgi_pr_open",
	"corgi_preview_start", "corgi_preview_freeze", "corgi_preview_stop",
	"corgi_worktrees_materialize", "corgi_worktrees_release",
}

func listRegisteredTools(t *testing.T) []mcp.Tool {
	t.Helper()
	s := newTestMCPServer()
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func boolHint(p *bool) (bool, bool) {
	if p == nil {
		return false, false
	}
	return *p, true
}

func TestMCPToolsAllTitledAndHinted(t *testing.T) {
	tools := listRegisteredTools(t)
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	destructive := map[string]bool{}
	for _, n := range tunnelGatedDestructive {
		destructive[n] = true
	}
	for _, tool := range tools {
		a := tool.Annotations
		if a.Title == "" {
			t.Errorf("%s: no title", tool.Name)
		}
		ro, roSet := boolHint(a.ReadOnlyHint)
		ds, dsSet := boolHint(a.DestructiveHint)
		if !roSet || !dsSet {
			t.Errorf("%s: readOnlyHint and destructiveHint must both be set", tool.Name)
			continue
		}
		if ro && ds {
			t.Errorf("%s: cannot be both read-only and destructive", tool.Name)
		}
		if destructive[tool.Name] && !ds {
			t.Errorf("%s: tunnel-gated tool must carry destructiveHint: true", tool.Name)
		}
		m := mcpToolMeta[tool.Name]
		if (m.Kind == toolReadOnly) != ro {
			t.Errorf("%s: readOnlyHint=%v does not match table kind %d", tool.Name, ro, m.Kind)
		}
		if (m.Kind == toolDestructive) != ds {
			t.Errorf("%s: destructiveHint=%v does not match table kind %d", tool.Name, ds, m.Kind)
		}
		if ow, _ := boolHint(a.OpenWorldHint); ow != m.OpenWorld {
			t.Errorf("%s: openWorldHint=%v, table says %v", tool.Name, ow, m.OpenWorld)
		}
	}
}

func TestMCPToolMetaMatchesRegistry(t *testing.T) {
	registered := map[string]bool{}
	for _, tool := range listRegisteredTools(t) {
		registered[tool.Name] = true
	}
	for name := range mcpToolMeta {
		if !registered[name] {
			t.Errorf("mcpToolMeta has %q but no such tool is registered", name)
		}
	}
	for name := range registered {
		if _, ok := mcpToolMeta[name]; !ok {
			t.Errorf("tool %q is registered without an mcpToolMeta entry", name)
		}
	}
}

func TestMCPToolsListOrderIsStable(t *testing.T) {
	first := listRegisteredTools(t)
	second := listRegisteredTools(t)
	if len(first) != len(second) {
		t.Fatalf("tool count changed between lists: %d vs %d", len(first), len(second))
	}
	names := make([]string, len(first))
	for i := range first {
		names[i] = first[i].Name
		if first[i].Name != second[i].Name {
			t.Fatalf("order differs at %d: %q vs %q", i, first[i].Name, second[i].Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("tools/list is not sorted by name: %v", names)
	}
}

func TestNewCorgiToolRefusesAnUnlistedTool(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unlisted tool must not register silently with mcp-go's destructive defaults")
		}
	}()
	newCorgiTool("corgi_not_in_the_table")
}
