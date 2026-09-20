package cmd

import (
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/workspace"
)

func claudeHome(t *testing.T) (agentD, mine, client string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	agentD, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	mine, client = t.TempDir(), t.TempDir()
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "mine", AbsPath: mine, ComposeFile: "corgi-compose.yml", Status: workspace.StatusOK})
	reg.Upsert(workspace.Workspace{ID: "client", AbsPath: client, ComposeFile: "corgi-compose.yml", Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(agentD), reg); err != nil {
		t.Fatal(err)
	}
	user := "version: 1\nworkspaces:\n  client:\n    configDir: " + filepath.Join(agentD, "claude-client") + "\n"
	if err := os.WriteFile(agentUserConfigPath(agentD), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	return agentD, mine, client
}

func TestWorkspaceRootFindsTheCheckoutById(t *testing.T) {
	_, mine, client := claudeHome(t)

	got, err := workspaceRoot("client")
	if err != nil {
		t.Fatal(err)
	}
	if cleanPath(got) != cleanPath(client) {
		t.Fatalf("client resolves to its own checkout: got %s want %s", got, client)
	}
	if got, _ := workspaceRoot("mine"); cleanPath(got) != cleanPath(mine) {
		t.Fatalf("mine resolves to its own checkout: got %s", got)
	}

	if _, err := workspaceRoot("nope"); err == nil {
		t.Fatal("an unregistered workspace is an error, not a silent fallback")
	} else if !strings.Contains(err.Error(), "not a registered workspace") {
		t.Fatalf("the error should say what is wrong: %v", err)
	}
}

func TestWorkspaceRootSaysWhenTheCheckoutIsGone(t *testing.T) {
	agentD, _, _ := claudeHome(t)
	gone := filepath.Join(t.TempDir(), "moved-away")
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "ghost", AbsPath: gone, ComposeFile: "corgi-compose.yml", Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(agentD), reg); err != nil {
		t.Fatal(err)
	}
	_, err := workspaceRoot("ghost")
	if err == nil || !strings.Contains(err.Error(), "not there") {
		t.Fatalf("a registered path that no longer exists must say so, got %v", err)
	}
}

func TestLaunchResolvesTheNamedWorkspaceNotTheCurrentFolder(t *testing.T) {
	agentD, mine, _ := claudeHome(t)

	fromWrongFolder, err := resolveClaudeLaunch(mine, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fromWrongFolder.Workspace != "mine" {
		t.Fatalf("without a workspace it still follows the folder: %+v", fromWrongFolder)
	}
	if fromWrongFolder.Env["CLAUDE_CONFIG_DIR"] != "" {
		t.Fatalf("mine has no account of its own: %+v", fromWrongFolder.Env)
	}

	root, err := workspaceRoot("client")
	if err != nil {
		t.Fatal(err)
	}
	launch, err := resolveClaudeLaunch(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Workspace != "client" {
		t.Fatalf("the named workspace wins: %+v", launch)
	}
	want := filepath.Join(agentD, "claude-client")
	if got := launch.Env["CLAUDE_CONFIG_DIR"]; got != want {
		t.Fatalf("the ticket must be worked on under that workspace's account: got %q want %q", got, want)
	}
}

func TestAgentCodexIsTheClaudeLaunchWithCodexAsTheHarness(t *testing.T) {
	_, mine, _ := claudeHome(t)
	defer func() { kindOverride = "" }()
	kindOverride = harness.Codex
	launch, err := resolveClaudeLaunch(mine, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Bin != "codex" || launch.Kind != supervisor.KindCodex || launch.Workspace != "mine" {
		t.Fatalf("codex in the folder's workspace: %+v", launch)
	}
	if !strings.HasPrefix(launch.String(), "codex") {
		t.Fatalf("--show prints the codex command: %s", launch.String())
	}
	cmd := agentCodexCmd
	if f := cmd.Flags().Lookup("workspace"); f == nil {
		t.Fatal("corgi agent codex takes the launch flags")
	}
	if f := cmd.Flags().Lookup("kind"); f == nil || !f.Hidden {
		t.Fatal("--kind is implied on corgi agent codex")
	}
}
