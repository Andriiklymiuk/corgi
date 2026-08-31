package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// runCLI drives the real cobra path — root flags merged, PersistentPreRun run —
// with the process exit captured instead of taken. exitProcess does not unwind,
// so a command that exits carries on; every assertion checks the code too.
func runCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	previousExit := osExit
	code := 0
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = previousExit })

	rootCmd.SetArgs(args)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	out := captureConsole(t, func() {
		if err := rootCmd.Execute(); err != nil {
			code = 1
		}
	})
	return out, code
}

func surfaceWorkspace(t *testing.T) string {
	t.Helper()
	dir := chdirToTempCompose(t, agentSurfaceCompose)
	if _, err := utils.GetCorgiServices(newRootedCmd()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunContextPrintsTheReport(t *testing.T) {
	surfaceWorkspace(t)
	contextNoGit = true
	t.Cleanup(func() { contextNoGit = false })

	out, code := runCLI(t, "context")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "surface") || !strings.Contains(out, "api") {
		t.Errorf("context output = %q", out)
	}
}

func TestRunWhyExitsNonZeroWhenNotHealthy(t *testing.T) {
	surfaceWorkspace(t)

	out, code := runCLI(t, "why", "api")
	if code != 1 {
		t.Errorf("a service that is not up must exit 1, got %d", code)
	}
	if !strings.Contains(out, "api →") {
		t.Errorf("why output = %q", out)
	}

	_, code = runCLI(t, "why", "ghost")
	if code != 1 {
		t.Errorf("an unknown service must exit 1, got %d", code)
	}
}

func TestRunEventsWithoutRunState(t *testing.T) {
	surfaceWorkspace(t)

	out, code := runCLI(t, "events")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "no run state") {
		t.Errorf("events output = %q", out)
	}
}

func TestRunEventsPrintsABaseline(t *testing.T) {
	dir := surfaceWorkspace(t)
	writeState(t, dir, []utils.RunStateEntry{
		{Name: "api", Kind: "service", Status: "running", PID: os.Getpid(), Port: 3300},
	}, nil)

	out, code := runCLI(t, "events")
	if code != 0 || !strings.Contains(out, "api") {
		t.Fatalf("exit %d, output %q", code, out)
	}
}

func TestRunLeasesListsAndReleases(t *testing.T) {
	surfaceWorkspace(t)

	out, code := runCLI(t, "leases")
	if code != 0 || !strings.Contains(out, "no leases") {
		t.Fatalf("empty listing: exit %d, output %q", code, out)
	}

	corgi, err := utils.GetCorgiServices(newRootedCmd())
	if err != nil {
		t.Fatal(err)
	}
	if err := utils.ApplyIsolationLease(corgi, "agent-a"); err != nil {
		t.Fatal(err)
	}

	out, code = runCLI(t, "leases")
	if code != 0 || !strings.Contains(out, "agent-a") || !strings.Contains(out, "+100") {
		t.Fatalf("listing: exit %d, output %q", code, out)
	}

	out, code = runCLI(t, "leases", "release", "agent-a")
	if code != 0 || !strings.Contains(out, "released") {
		t.Fatalf("release: exit %d, output %q", code, out)
	}
	if _, code = runCLI(t, "leases", "release", "agent-a"); code != 1 {
		t.Errorf("releasing twice must exit 1, got %d", code)
	}
}

func TestRunCheckpointListAndRemove(t *testing.T) {
	needGit(t)
	dir := surfaceWorkspace(t)
	newRepo(t, dir)

	out, code := runCLI(t, "checkpoint", "list")
	if code != 0 || !strings.Contains(out, "no checkpoints") {
		t.Fatalf("empty: exit %d, output %q", code, out)
	}

	out, code = runCLI(t, "checkpoint", "cp-run")
	if code != 0 || !strings.Contains(out, "cp-run") {
		t.Fatalf("create: exit %d, output %q", code, out)
	}

	out, code = runCLI(t, "checkpoint", "list")
	if code != 0 || !strings.Contains(out, "cp-run") {
		t.Fatalf("list: exit %d, output %q", code, out)
	}

	out, code = runCLI(t, "checkpoint", "rm", "cp-run")
	if code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("remove: exit %d, output %q", code, out)
	}
	if _, err := os.Stat(checkpointPath("cp-run")); !os.IsNotExist(err) {
		t.Error("the checkpoint file should be gone")
	}
	if _, code = runCLI(t, "checkpoint", "rm", "cp-run"); code != 1 {
		t.Errorf("removing twice must exit 1, got %d", code)
	}
}

func TestRunCheckpointRejectsABadName(t *testing.T) {
	surfaceWorkspace(t)
	if _, code := runCLI(t, "checkpoint", "../escape"); code != 2 {
		t.Errorf("a bad checkpoint name must exit 2, got %d", code)
	}
}

func TestRunRestorePutsTheTreeBack(t *testing.T) {
	needGit(t)
	dir := surfaceWorkspace(t)
	newRepo(t, dir)
	if _, code := runCLI(t, "checkpoint", "cp-restore"); code != 0 {
		t.Fatal("checkpoint failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, "restore", "cp-restore", "--yes")
	if code != 0 {
		t.Fatalf("restore exit %d, output %q", code, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(body) != "a\n" {
		t.Fatalf("restored content = %q (%v)", body, err)
	}

	if _, code := runCLI(t, "restore", "never-made", "--yes"); code != 1 {
		t.Errorf("restoring an unknown checkpoint must exit 1, got %d", code)
	}
}

func TestConfirmRestoreRefusesWithoutATerminal(t *testing.T) {
	previous := utils.NonInteractive
	utils.NonInteractive = true
	t.Cleanup(func() { utils.NonInteractive = previous })

	out := captureConsole(t, func() {
		if confirmRestore([]string{"api"}, "safety-1") {
			t.Error("a non-interactive restore must not proceed without --yes")
		}
	})
	if !strings.Contains(out, "--yes") {
		t.Errorf("it must say how to proceed: %q", out)
	}
}
