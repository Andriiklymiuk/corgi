package cmd

import (
	"os"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
)

// registerStack rewrites the whole registry, so several workspaces have to be
// written in one go.
func registerStacks(t *testing.T, agentDir string, stacks map[string]string) {
	t.Helper()
	reg := &workspace.Registry{}
	for id, path := range stacks {
		reg.Upsert(workspace.Workspace{
			ID: id, AbsPath: path, ComposeFile: "corgi-compose.yml", Status: workspace.StatusOK,
		})
	}
	if err := workspace.Save(agentRegistryPath(agentDir), reg); err != nil {
		t.Fatal(err)
	}
}

func allFlagCmd(t *testing.T, all bool) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "enable"}
	c.Flags().Bool("all", false, "")
	if all {
		if err := c.Flags().Set("all", "true"); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestAgentHooksEnableAllCoversEveryWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()

	api := stackWithAgentConfig(t, "")
	web := stackWithAgentConfig(t, "")
	registerStacks(t, agentD, map[string]string{"api": api, "web": web})

	// --all must not depend on where it is run from.
	cwd, _ := os.Getwd()
	away := t.TempDir()
	if err := os.Chdir(away); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runAgentHooksEnable(allFlagCmd(t, true), nil)

	for id, stack := range map[string]string{"api": api, "web": web} {
		hooks, _ := readJSONObject(claudeLocalSettingsPath(stack))["hooks"].(map[string]any)
		if hooks == nil || len(hooks) != 2 {
			t.Fatalf("%s: both events must be hooked, got %v", id, hooks)
		}
		if !strings.Contains(marshalCompact(hooks), "--workspace "+id) {
			t.Errorf("%s: hook must name its own workspace: %s", id, marshalCompact(hooks))
		}
	}

	runAgentHooksDisable(allFlagCmd(t, true), nil)
	for id, stack := range map[string]string{"api": api, "web": web} {
		if hooks, ok := readJSONObject(claudeLocalSettingsPath(stack))["hooks"]; ok {
			t.Errorf("%s: hooks should be gone, got %v", id, hooks)
		}
	}
}

func TestAgentHooksEnableAllRefusesWithNoWorkspaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)

	previous := osExit
	code := 0
	osExit = func(c int) { code = c; panic("exit") }
	t.Cleanup(func() { osExit = previous })

	defer func() {
		_ = recover()
		if code != 2 {
			t.Errorf("an empty registry must exit 2, got %d", code)
		}
	}()
	runAgentHooksEnable(allFlagCmd(t, true), nil)
}
