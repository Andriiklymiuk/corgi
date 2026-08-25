package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/workspace"
)

// stackWithAgentConfig writes a minimal corgi stack whose .corgi/agent.yml
// carries the given body, and returns its absolute path.
func stackWithAgentConfig(t *testing.T, agentYML string) string {
	t.Helper()
	stack := t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "corgi-compose.yml"),
		[]byte("name: s\nservices:\n  api:\n    path: .\n    manualRun: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if agentYML != "" {
		if err := os.MkdirAll(filepath.Join(stack, ".corgi"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stack, ".corgi", "agent.yml"), []byte(agentYML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return stack
}

// registerStack writes a one-workspace registry under agentDir pointing at
// stack, and returns the workspace id.
func registerStack(t *testing.T, agentDir, id, stack string) {
	t.Helper()
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: id, AbsPath: stack, ComposeFile: "corgi-compose.yml", Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(agentDir), reg); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteResolverRefusesASensitiveWorkspace(t *testing.T) {
	agentDir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n  sensitive: true\n")
	registerStack(t, agentDir, "acme", stack)

	_, err := remoteResolver(agentDir, false)("acme", "")

	if err == nil {
		t.Fatal("a sensitive workspace must refuse remote session start")
	}
	if !strings.Contains(err.Error(), "sensitive") {
		t.Errorf("error should name the reason, got %q", err)
	}
}

func TestRemoteResolverStartsANonSensitiveWorkspace(t *testing.T) {
	agentDir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n")
	registerStack(t, agentDir, "acme", stack)

	cfg, err := remoteResolver(agentDir, false)("acme", "")

	if err != nil {
		t.Fatalf("a normal workspace must resolve, got %v", err)
	}
	if cfg.Dir != stack {
		t.Errorf("dir = %q, want the registered path", cfg.Dir)
	}
	if cfg.Origin != "remote" {
		t.Errorf("origin = %q, want remote", cfg.Origin)
	}
}

func TestRemoteResolverRejectsAnUnknownWorkspace(t *testing.T) {
	agentDir := t.TempDir()
	registerStack(t, agentDir, "acme", stackWithAgentConfig(t, ""))

	_, err := remoteResolver(agentDir, false)("ghost", "")

	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("an unknown workspace must error by name, got %v", err)
	}
}
