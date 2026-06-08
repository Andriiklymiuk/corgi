//go:build !windows

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newTestMCPServer builds a server with the real tool+resource registration so
// we exercise registerMCPTools/registerMCPResources end to end.
func newTestMCPServer() *server.MCPServer {
	s := server.NewMCPServer("corgi-test", "0.0.0")
	registerMCPTools(s)
	registerMCPResources(s)
	return s
}

// TestRegisterMCPTools_ListsExpectedTools drives the in-process client to list
// the registered tools, proving registration wired them up.
func TestRegisterMCPTools_ListsExpectedTools(t *testing.T) {
	s := newTestMCPServer()
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{
		"corgi_validate", "corgi_plan", "corgi_status", "corgi_env", "corgi_ps",
		"corgi_up", "corgi_down", "corgi_logs", "corgi_exec", "corgi_test",
		"corgi_doctor", "corgi_restart", "corgi_db_query", "corgi_schema",
	} {
		if !got[want] {
			t.Errorf("tool %q not registered (registered: %v)", want, got)
		}
	}
}

// TestRegisterMCPResources_ListsExpectedResources proves the four resources are
// registered and that the schema resource returns ComposeJSONSchema().
func TestRegisterMCPResources_ListsExpectedResources(t *testing.T) {
	s := newTestMCPServer()
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	res, err := c.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	got := map[string]bool{}
	for _, r := range res.Resources {
		got[r.URI] = true
	}
	for _, want := range []string{"corgi://schema", "corgi://drivers", "corgi://compose", "corgi://status"} {
		if !got[want] {
			t.Errorf("resource %q not registered (registered: %v)", want, got)
		}
	}

	read, err := c.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: "corgi://schema"},
	})
	if err != nil {
		t.Fatalf("read schema resource: %v", err)
	}
	if len(read.Contents) == 0 {
		t.Fatal("schema resource returned no contents")
	}
	tc, ok := read.Contents[0].(mcp.TextResourceContents)
	if !ok || tc.Text != utils.ComposeJSONSchema() {
		t.Errorf("schema resource text does not match ComposeJSONSchema()")
	}
}

// TestServeMCPStdioEndToEnd exercises the registered corgi_validate tool through
// the in-memory transport — the same path serveMCPStdio serves, minus the pipe.
func TestServeMCPStdioEndToEnd(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	s := newTestMCPServer()
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	call := mcp.CallToolRequest{}
	call.Params.Name = "corgi_validate"
	res, err := c.CallTool(ctx, call)
	if err != nil {
		t.Fatalf("call corgi_validate: %v", err)
	}
	if res.IsError {
		t.Fatalf("corgi_validate returned tool error: %+v", res.Content)
	}
	txt, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	var got validateResult
	if err := json.Unmarshal([]byte(txt.Text), &got); err != nil {
		t.Fatalf("unmarshal result: %v (raw=%q)", err, txt.Text)
	}
	if !got.Ok {
		t.Errorf("expected ok=true for the valid fixture, got %+v", got)
	}
}

// --- jsonHandler (marshals result, converts error to tool error) ---

func TestJSONHandler_MarshalsResult(t *testing.T) {
	h := jsonHandler(func(mcp.CallToolRequest) (any, error) {
		return map[string]string{"hello": "world"}, nil
	})
	res, err := h(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	txt := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(txt, `"hello":"world"`) {
		t.Errorf("marshaled text = %q", txt)
	}
}

func TestJSONHandler_ConvertsErrorToToolError(t *testing.T) {
	h := jsonHandler(func(mcp.CallToolRequest) (any, error) {
		return nil, errString("kaboom")
	})
	res, err := h(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler should not return a go error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true when core returns an error")
	}
	if !strings.Contains(res.Content[0].(mcp.TextContent).Text, "kaboom") {
		t.Errorf("error text lost: %+v", res.Content)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// --- isAlreadyRunning ---

func TestIsAlreadyRunning_NoStateFile(t *testing.T) {
	dir := t.TempDir()
	if isAlreadyRunning(utils.RunStatePath(dir)) {
		t.Error("no state file must report not-running")
	}
}

func TestIsAlreadyRunning_StoppedState(t *testing.T) {
	dir := t.TempDir()
	statePath := utils.RunStatePath(dir)
	if err := utils.WriteRunState(statePath, utils.RunState{
		Services: []utils.RunStateEntry{{Name: "api", Kind: "service", PID: 999999, Status: "stopped"}},
	}); err != nil {
		t.Fatal(err)
	}
	// PID 999999 won't be alive → reconcile leaves it non-running.
	if isAlreadyRunning(statePath) {
		t.Error("a stopped/dead service must report not-running")
	}
}

// --- printMCPClientConfig (header only when token set) ---

func TestPrintMCPClientConfig(t *testing.T) {
	var noTok bytes.Buffer
	printMCPClientConfig(&noTok, "http://127.0.0.1:8765/mcp", "")
	if strings.Contains(noTok.String(), "Authorization") {
		t.Errorf("no-token config must omit Authorization: %s", noTok.String())
	}
	if !strings.Contains(noTok.String(), "http://127.0.0.1:8765/mcp") {
		t.Errorf("config missing url: %s", noTok.String())
	}

	var withTok bytes.Buffer
	printMCPClientConfig(&withTok, "https://x/mcp", "corgi_mcp_abc")
	var parsed map[string]any
	if err := json.Unmarshal(withTok.Bytes(), &parsed); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	if !strings.Contains(withTok.String(), "Bearer corgi_mcp_abc") {
		t.Errorf("token config must include the bearer header: %s", withTok.String())
	}
}

// --- tailLogFile ---

func TestTailLogFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.log")
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lines, err := tailLogFile(p, 2)
	if err != nil {
		t.Fatalf("tailLogFile: %v", err)
	}
	if len(lines) != 2 || lines[0] != "c" || lines[1] != "d" {
		t.Errorf("expected last 2 lines [c d], got %v", lines)
	}

	// Empty file → empty slice, no error.
	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := tailLogFile(empty, 10)
	if err != nil || len(got) != 0 {
		t.Errorf("empty file: got %v err %v", got, err)
	}

	// Missing file → error.
	if _, err := tailLogFile(filepath.Join(dir, "nope.log"), 5); err == nil {
		t.Error("expected error for missing file")
	}
}

// --- mcpUp already-running guard (no real spawn) ---

func TestMCPUp_BlockedWhenAlreadyRunning(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	// mcpUp resolves the state path off utils.CorgiComposePathDir (an absolute
	// path), so load once to populate it and seed the state where mcpUp reads it.
	if _, err := loadComposeForMCP(""); err != nil {
		t.Fatalf("load compose: %v", err)
	}
	// PidAlive only counts a pid that is its own process-group leader as alive,
	// so seed with a live group-leader child rather than os.Getpid() (the test
	// process isn't a group leader under `go test`).
	pid := spawnGroupLeader(t)
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if err := utils.WriteRunState(statePath, utils.RunState{
		Services: []utils.RunStateEntry{{Name: "api", Kind: "service", PID: pid, Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpUp(upArgs{}); err == nil {
		t.Error("mcpUp must error when a service is already running")
	} else if !strings.Contains(err.Error(), string(utils.ErrAlreadyRunning)) {
		t.Errorf("expected ErrAlreadyRunning, got %v", err)
	}
}

// spawnGroupLeader starts a long-lived sleep in its own process group and
// returns its pid; killed on cleanup. Mirrors how detached procs look to
// PidAlive (pgid == pid).
func spawnGroupLeader(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn group leader: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}
