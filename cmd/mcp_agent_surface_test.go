package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const agentSurfaceCompose = `name: surface
services:
  api:
    port: 3300
    start:
      - go run .
  web:
    port: 3301
    start:
      - npm start
`

func TestMCPContextReportsTopology(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)

	got, err := mcpContext(contextArgs{NoGit: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace != "surface" || len(got.Services) != 2 {
		t.Fatalf("context = %+v", got)
	}
	if got.Services[0].Name != "api" || got.Services[0].Port != 3300 {
		t.Errorf("first service = %+v", got.Services[0])
	}
	if got.Services[0].Repo != nil {
		t.Error("noGit must leave repo state out")
	}
	if contextNoGit {
		t.Error("the noGit flag must be restored after the call")
	}
}

func TestMCPWhyReturnsAVerdict(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)

	got, err := mcpWhy(whyArgs{Service: "api", LogLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got.Service != "api" || got.Verdict == "" || got.Detail == "" {
		t.Fatalf("why = %+v", got)
	}
	if whyLogLines != 8 {
		t.Errorf("logLines must be restored, got %d", whyLogLines)
	}

	if _, err := mcpWhy(whyArgs{Service: "ghost"}); err == nil {
		t.Error("an unknown service must be an error")
	}
}

func TestMCPWaitForLogMatchesAndValidates(t *testing.T) {
	dir := chdirToTempCompose(t, agentSurfaceCompose)

	logDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "run.log"), []byte("Listening on :3300\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := mcpWaitForLog(waitForLogArgs{Service: "api", Pattern: "Listening on", TimeoutSec: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matched || !strings.Contains(got.Line, "Listening on") {
		t.Fatalf("result = %+v", got)
	}

	if _, err := mcpWaitForLog(waitForLogArgs{Service: "api"}); err == nil {
		t.Error("a missing pattern must be rejected")
	}
}

func TestMCPCheckoutReportsEveryRepo(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)

	got, err := mcpCheckout(checkoutArgs{Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected the workspace plus both services, got %d rows", len(got))
	}
	for _, row := range got {
		if row.Status == "" {
			t.Errorf("row %s has no status", row.Name)
		}
	}
	if checkoutAllowDirty {
		t.Error("allowDirty must be restored after the call")
	}
}

func TestMCPCheckpointAndRestoreRoundTrip(t *testing.T) {
	needGit(t)
	dir := chdirToTempCompose(t, agentSurfaceCompose)
	newRepo(t, dir)

	made, err := mcpCheckpoint(checkpointArgs{Name: "cp-one"})
	if err != nil {
		t.Fatal(err)
	}
	if made.Name != "cp-one" || len(made.Repos) == 0 {
		t.Fatalf("checkpoint = %+v", made)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	restored, err := mcpRestore(checkpointArgs{Name: "cp-one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Restored) == 0 || len(restored.Failed) != 0 {
		t.Fatalf("restore = %+v", restored)
	}
	if restored.Safety == "" {
		t.Error("a dirty tree must be captured under a named safety checkpoint")
	}
	body, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(body) != "a\n" {
		t.Fatalf("restored content = %q (%v)", body, err)
	}
}

func TestMCPCheckpointRejectsABadName(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)
	if _, err := mcpCheckpoint(checkpointArgs{Name: "../escape"}); err == nil {
		t.Error("a checkpoint name with a path separator must be rejected")
	}
}

func TestMCPRestoreRejectsAnUnknownCheckpoint(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)
	if _, err := mcpRestore(checkpointArgs{Name: "never-made"}); err == nil {
		t.Error("restoring an unknown checkpoint must error")
	}
}

func TestRegisterAgentSurfaceToolsAddsEveryTool(t *testing.T) {
	s := server.NewMCPServer("corgi-test", APP_VERSION)
	registerAgentSurfaceTools(s, mcp.WithString("composePath", mcp.Description("compose path")))

	res := s.HandleMessage(context.Background(), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	body, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	listed := string(body)
	for _, name := range []string{
		"corgi_context", "corgi_why", "corgi_wait_for_log",
		"corgi_checkout", "corgi_checkpoint", "corgi_restore",
	} {
		if !strings.Contains(listed, name) {
			t.Errorf("%s was not registered", name)
		}
	}
}

func TestMCPCheckpointRejectsATraversalName(t *testing.T) {
	chdirToTempCompose(t, agentSurfaceCompose)
	for _, name := range []string{"..", "../escape", ".hidden"} {
		if _, err := mcpCheckpoint(checkpointArgs{Name: name}); err == nil {
			t.Errorf("checkpoint name %q must be rejected", name)
		}
		if _, err := mcpRestore(checkpointArgs{Name: name}); err == nil {
			t.Errorf("restore name %q must be rejected", name)
		}
	}
}
