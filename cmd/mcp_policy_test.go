package cmd

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// An agent client reads a workspace's switches and flips the live ones
// through the same code the phone uses; the mute and the lessons answer
// in kind.
func TestMCPSwitchesMuteAndLessons(t *testing.T) {
	dir := phoneBoard(t, true)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": "api", "autoAllow": "reads", "doneWhen": []any{"go test ./...", ""}, "compactAt": float64(85), "lessons": true}
	out, err := mcpWatchSet(req)
	if err != nil {
		t.Fatal(err)
	}
	sw := out.(map[string]any)["watch"].(WatchSwitches)
	if sw.AutoAllow != "reads" || len(sw.DoneWhen) != 1 || sw.CompactAt != 85 || !sw.Lessons || sw.Rebase {
		t.Fatalf("%+v", sw)
	}
	got, _ := mcpWatchSwitches("api")
	if list := got.(map[string]any)["workspaces"].([]WatchSwitches); len(list) != 1 || list[0].CompactAt != 85 {
		t.Fatalf("%+v", list)
	}
	req.Params.Arguments = map[string]any{"workspace": "api", "autoAllow": "everything"}
	if _, err := mcpWatchSet(req); err == nil {
		t.Fatal("only reads")
	}
	m, _ := mcpMute("30m")
	if !m.(map[string]any)["muted"].(bool) {
		t.Fatal("muted")
	}
	m, _ = mcpMute("off")
	if m.(map[string]any)["muted"].(bool) {
		t.Fatal("off")
	}
	if _, err := mcpMute("2d"); err == nil {
		t.Fatal("too long")
	}
	l, err := mcpLessons("api", "never mock the database")
	if err != nil {
		t.Fatal(err)
	}
	if list := l.(map[string]any)["lessons"].([]lessons.Lesson); len(list) != 1 || list[0].Source != "agent" {
		t.Fatalf("%+v", list)
	}
}
