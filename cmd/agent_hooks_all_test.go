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

func allFlagCmd(t *testing.T, flags ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "enable"}
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("turns", false, "")
	for _, name := range flags {
		if err := c.Flags().Set(name, "true"); err != nil {
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

	runAgentHooksEnable(allFlagCmd(t, "all"), nil)

	for id, stack := range map[string]string{"api": api, "web": web} {
		hooks, _ := readJSONObject(claudeLocalSettingsPath(stack))["hooks"].(map[string]any)
		if hooks == nil || hooks[hookEventNotification] == nil {
			t.Fatalf("%s: the needs-you event must be hooked, got %v", id, hooks)
		}
		if hooks[hookEventStop] != nil {
			t.Errorf("%s: turn-end must stay off unless --turns is passed, got %v", id, hooks[hookEventStop])
		}
		if !strings.Contains(marshalCompact(hooks), "--workspace "+id) {
			t.Errorf("%s: hook must name its own workspace: %s", id, marshalCompact(hooks))
		}
	}

	runAgentHooksDisable(allFlagCmd(t, "all"), nil)
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
	runAgentHooksEnable(allFlagCmd(t, "all"), nil)
}

func TestAgentHooksTurnsIsOptInAndReversible(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStacks(t, agentD, map[string]string{"api": stack})

	cwd, _ := os.Getwd()
	if err := os.Chdir(stack); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runAgentHooksEnable(allFlagCmd(t, "turns"), nil)
	hooks, _ := readJSONObject(claudeLocalSettingsPath(stack))["hooks"].(map[string]any)
	if hooks[hookEventStop] == nil {
		t.Fatalf("--turns must hook the turn end, got %v", hooks)
	}

	runAgentHooksEnable(allFlagCmd(t), nil)
	hooks, _ = readJSONObject(claudeLocalSettingsPath(stack))["hooks"].(map[string]any)
	if hooks[hookEventStop] != nil {
		t.Errorf("re-running without --turns must take the turn-end hook back out, got %v", hooks[hookEventStop])
	}
	if hooks[hookEventNotification] == nil {
		t.Error("the needs-you hook must survive")
	}
}

func TestAgentHooksEnableKeepsOtherHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStacks(t, agentD, map[string]string{"api": stack})

	path := claudeLocalSettingsPath(stack)
	if err := writeJSONObject(path, map[string]any{
		"hooks": map[string]any{"Stop": []any{
			map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo mine"}}},
		}},
		"other": "kept",
	}); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(stack); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runAgentHooksEnable(allFlagCmd(t), nil)

	after := readJSONObject(path)
	if after["other"] != "kept" {
		t.Error("unrelated settings must survive")
	}
	hooks, _ := after["hooks"].(map[string]any)
	if !strings.Contains(marshalCompact(hooks[hookEventStop]), "echo mine") {
		t.Errorf("someone else's Stop hook must survive: %v", hooks[hookEventStop])
	}
}
