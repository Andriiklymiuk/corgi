//go:build !windows

package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

func TestMCPCallBudgetEnv(t *testing.T) {
	t.Setenv("CORGI_MCP_CALL_BUDGET", "")
	if got := mcpCallBudget(); got != defaultMCPCallBudget {
		t.Errorf("default = %s", got)
	}
	t.Setenv("CORGI_MCP_CALL_BUDGET", "90s")
	if got := mcpCallBudget(); got != 90*time.Second {
		t.Errorf("override = %s", got)
	}
	t.Setenv("CORGI_MCP_CALL_BUDGET", "5ms")
	if got := mcpCallBudget(); got != defaultMCPCallBudget {
		t.Errorf("a budget too small to run anything must fall back, got %s", got)
	}
}

func TestClampToBudget(t *testing.T) {
	t.Setenv("CORGI_MCP_CALL_BUDGET", "30s")
	if d, clamped := clampToBudget(10 * time.Second); d != 10*time.Second || clamped {
		t.Errorf("under budget: %s clamped=%v", d, clamped)
	}
	if d, clamped := clampToBudget(time.Hour); d != 30*time.Second || !clamped {
		t.Errorf("over budget: %s clamped=%v", d, clamped)
	}
	if d, clamped := clampToBudget(0); d != 30*time.Second || clamped {
		t.Errorf("zero means the whole budget without counting as clamped: %s clamped=%v", d, clamped)
	}
}

// A caller can ask corgi_wait_for_log for an hour; the connector drops the
// call at four minutes, so the wait ends at the budget and says so.
func TestWaitForLogClampsToTheBudget(t *testing.T) {
	dir := chdirToTempCompose(t, agentSurfaceCompose)
	logDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "run.log"), []byte("booting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORGI_MCP_CALL_BUDGET", "1s")

	started := time.Now()
	got, err := mcpWaitForLog(waitForLogArgs{Service: "api", Pattern: "never appears", TimeoutSec: 9999})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("waited %s; the budget must end the wait", elapsed)
	}
	if got.Matched || !got.TimedOut {
		t.Errorf("result = %+v, want timedOut without a match", got)
	}
}

func TestRunServiceCommandExitCodeContextKillsAtTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	started := time.Now()
	code, err := utils.RunServiceCommandExitCodeContext(ctx, "sleep 30", t.TempDir(), false, &out, &out)
	if err != context.DeadlineExceeded || code != -1 {
		t.Fatalf("code=%d err=%v, want -1 and DeadlineExceeded", code, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("took %s; the process group must be killed at the deadline", elapsed)
	}
}

func fakeUpChild(t *testing.T, script string) {
	t.Helper()
	previous := mcpUpChildCommand
	mcpUpChildCommand = func(upArgs, string, string) (*exec.Cmd, error) {
		return exec.Command("sh", "-c", script), nil
	}
	t.Cleanup(func() {
		mcpUpChildCommand = previous
		mcpUpInFlightMu.Lock()
		for dir, h := range mcpUpInFlight {
			_ = utils.KillProcessGroup(h.PID)
			delete(mcpUpInFlight, dir)
		}
		mcpUpInFlightMu.Unlock()
	})
}

func TestMCPUpDetachedHandsBackAHandleWhileTheChildBoots(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	fakeUpChild(t, "sleep 30")

	got, err := mcpUpDetached(upArgs{}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != upStatusStarting || got.Handle == nil || got.Handle.PID <= 0 || got.Next == "" {
		t.Fatalf("result = %+v, want starting with a handle", got)
	}
	if _, err := os.Stat(got.Handle.LogPath); err != nil {
		t.Errorf("boot log %s must exist: %v", got.Handle.LogPath, err)
	}
	ignore, _ := os.ReadFile(filepath.Join(filepath.Dir(got.Handle.LogPath), ".gitignore"))
	if !strings.Contains(string(ignore), "mcp-up-*.log") {
		t.Error("the boot log must be ignored under corgi_services")
	}

	again, err := mcpUpDetached(upArgs{}, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != upStatusStarting || again.Handle == nil || again.Handle.PID != got.Handle.PID {
		t.Errorf("a second call during the boot must return the same handle, got %+v", again)
	}
}

func TestMCPUpDetachedReturnsTheRunStateWhenTheChildFinishes(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	if _, err := loadComposeForMCP(""); err != nil {
		t.Fatal(err)
	}
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	fixture := filepath.Join(t.TempDir(), "state.json")
	if err := utils.WriteRunState(fixture, utils.RunState{
		Services: []utils.RunStateEntry{{Name: "api", Kind: "service", PID: 424242, Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	fakeUpChild(t, "echo booted; mkdir -p '"+filepath.Dir(statePath)+"' && cp '"+fixture+"' '"+statePath+"'")

	got, err := mcpUpDetached(upArgs{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != upStatusStarted || got.State == nil || len(got.State.Services) != 1 || got.State.Services[0].Name != "api" {
		t.Fatalf("result = %+v, want started with the run-state", got)
	}
}

func TestMCPUpDetachedReportsAFailedChild(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	fakeUpChild(t, "echo 'boom: beforeStart failed' >&2; exit 3")

	got, err := mcpUpDetached(upArgs{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != upStatusFailed || !strings.Contains(got.Error, "boom") {
		t.Fatalf("result = %+v, want failed with the log tail", got)
	}
}

func TestMCPUpDetachedBlockedWhenAlreadyRunning(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	if _, err := loadComposeForMCP(""); err != nil {
		t.Fatal(err)
	}
	pid := spawnGroupLeader(t)
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if err := utils.WriteRunState(statePath, utils.RunState{
		Services: []utils.RunStateEntry{{Name: "api", Kind: "service", PID: pid, Status: "running"}},
	}); err != nil {
		t.Fatal(err)
	}
	fakeUpChild(t, "exit 0")
	if _, err := mcpUpDetached(upArgs{}, time.Second); err == nil || !strings.Contains(err.Error(), string(utils.ErrAlreadyRunning)) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
}

func TestMCPUpChildCommandFlags(t *testing.T) {
	cmd, err := mcpUpChildCommand(upArgs{Profile: "web", Omit: "beforeStart", Seed: true, ServiceDir: "api=/tmp/api", ServiceBranch: "api=fix"}, "/w/corgi-compose.yml", "/w/log")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(cmd.Args[1:], " ")
	for _, want := range []string{"run --detach --ci --logs -f /w/corgi-compose.yml", "--profile web", "--omit beforeStart", "--seed", "--service-dir api=/tmp/api", "--service-branch api=fix"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	if cmd.Dir != "/w" {
		t.Errorf("child must run in the compose dir, got %q", cmd.Dir)
	}
}
