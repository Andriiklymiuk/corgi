package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestDirIsWorkspace(t *testing.T) {
	compose := t.TempDir()
	if err := os.WriteFile(filepath.Join(compose, "corgi-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir() // a .git FILE marks a git worktree/submodule
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !dirIsWorkspace(compose) {
		t.Error("a corgi stack is a workspace")
	}
	if !dirIsWorkspace(gitRepo) {
		t.Error("a plain git repo is a workspace — Remote Control is useful without a compose file")
	}
	if !dirIsWorkspace(worktree) {
		t.Error("a git worktree (.git file) is a workspace")
	}
	if dirIsWorkspace(t.TempDir()) {
		t.Error("an arbitrary folder is NOT a workspace — that guard must not regress")
	}
	if dirIsWorkspace("") {
		t.Error("empty dir is not a workspace")
	}
}

func TestCorgiListenerPIDsIsSafeOnBadInput(t *testing.T) {
	if pids := corgiListenerPIDs("not-an-addr"); pids != nil {
		t.Errorf("malformed addr must yield nothing, got %v", pids)
	}
	// Port 1 is never held by a corgi process; lsof finding nothing must mean
	// "no one to stop", not an error.
	if pids := corgiListenerPIDs("127.0.0.1:1"); len(pids) != 0 {
		t.Errorf("an unheld port must yield nothing, got %v", pids)
	}
}

func TestReclaimCorgiMCPWithNothingListening(t *testing.T) {
	if reclaimCorgiMCP("127.0.0.1:1") {
		t.Error("nothing corgi-owned on the port — reclaim must report not-reclaimed")
	}
}

func TestWorkspaceSessionTarget(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	agentD := mustAgentDir()

	if _, _, ok := workspaceSessionTarget("nope"); ok {
		t.Error("an unknown workspace must not resolve")
	}

	stack := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "acme", stack)
	absPath, configDir, ok := workspaceSessionTarget("acme")
	if !ok || absPath != stack {
		t.Fatalf("workspaceSessionTarget = %q, %v; want the registry path", absPath, ok)
	}
	if configDir != "" {
		t.Errorf("no user config → default account, got %q", configDir)
	}
}

func TestListClaudeSessionsWithoutTheBinary(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	sessions := listClaudeSessions(t.TempDir(), "")
	if sessions == nil || len(sessions) != 0 {
		t.Errorf("a missing claude binary must mean an empty list, not an error: %v", sessions)
	}
}

func TestLaunchSessionsHandlerUnknownWorkspace(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchSessionsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/sessions?workspace=ghost", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown workspace must be 404, got %d", rec.Code)
	}
}

func TestLaunchSessionsHandlerListsForARegisteredWorkspace(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	t.Setenv("PATH", "/nonexistent") // no claude binary → empty list, still 200
	registerStack(t, mustAgentDir(), "acme", stackWithAgentConfig(t, ""))

	rec := httptest.NewRecorder()
	launchSessionsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/sessions?workspace=acme", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("a registered workspace must answer 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddProfileRejectsSkipPlusPermissionMode(t *testing.T) {
	err := addProfile(t.TempDir(), "both", config.WorkspaceConfig{
		ConfigDir: "~/.claude-x", PermissionMode: "acceptEdits", DangerouslySkipPermissions: true,
	})
	if err == nil {
		t.Error("a profile with both a permission mode and the bypass is ambiguous and must be rejected at add time")
	}
}

func TestEnableWorkspaceSkipPermissionsIsSticky(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	agentD := mustAgentDir()

	if err := enableWorkspace("acme", "~/.claude-x", true); err != nil {
		t.Fatal(err)
	}
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil || !user.Workspaces["acme"].DangerouslySkipPermissions {
		t.Fatalf("init --dangerously-skip-permissions must persist: %+v, %v", user, err)
	}

	// A later re-init WITHOUT the flag must not silently clear the granted bypass.
	if err := enableWorkspace("acme", "", false); err != nil {
		t.Fatal(err)
	}
	user, _ = config.LoadUser(agentUserConfigPath(agentD))
	if !user.Workspaces["acme"].DangerouslySkipPermissions {
		t.Error("a re-init without the flag must leave a previously-granted bypass alone")
	}
	if user.Workspaces["acme"].ConfigDir != "~/.claude-x" {
		t.Error("a re-init without --config-dir must keep the existing one")
	}
}
