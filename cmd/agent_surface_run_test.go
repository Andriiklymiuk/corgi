package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	t.Cleanup(resetCommandFlags)
	resetCommandFlags()
	activeLogFilter = logStreamFilter{}

	out := captureConsole(t, func() {
		if err := rootCmd.Execute(); err != nil {
			code = 1
		}
	})
	return out, code
}

// cobra keeps parsed flag values on the shared rootCmd tree between Execute
// calls, so one test's --wait-for would still be set for the next one.
func resetCommandFlags() {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Changed {
				_ = f.Value.Set(f.DefValue)
				f.Changed = false
			}
		})
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
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

func TestRunEnvExplainShowsTheChain(t *testing.T) {
	chdirToTempCompose(t, `name: explain
services:
  api:
    port: 4400
    environment:
      - DATABASE_URL=postgres://from-compose
    start:
      - go run .
`)
	out, code := runCLI(t, "env", "api", "--explain", "DATABASE_URL")
	if code != 0 {
		t.Fatalf("exit %d, out %q", code, out)
	}
	if !strings.Contains(out, "api · DATABASE_URL") || !strings.Contains(out, "literal") {
		t.Errorf("explain output = %q", out)
	}

	out, code = runCLI(t, "env", "api", "--explain", "NOT_SET")
	if code != 0 || !strings.Contains(out, "nothing sets this variable") {
		t.Errorf("unset key: exit %d, out %q", code, out)
	}

	out, code = runCLI(t, "env", "api", "--explain", "DATABASE_URL", "--reveal")
	if code != 0 || !strings.Contains(out, "postgres://from-compose") {
		t.Errorf("--reveal must show the real value: %q", out)
	}
}

func TestRunLogsWaitForMatchesAndTimesOut(t *testing.T) {
	dir := chdirToTempCompose(t, agentSurfaceCompose)
	logDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "2026-01-01T00:00:00.000Z booting\n2026-01-01T00:00:01.000Z Listening on :3300\n"
	if err := os.WriteFile(filepath.Join(logDir, "2026-01-01_000000.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, code := runCLI(t, "logs", "--service", "api", "--wait-for", "Listening on", "--timeout", "5s")
	if code != 0 {
		t.Errorf("a match in the running run must exit 0, got %d", code)
	}

	if err := os.WriteFile(filepath.Join(logDir, "2026-01-02_000000.crashed.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code = runCLI(t, "logs", "--service", "api", "--wait-for", "Listening on", "--timeout", "600ms")
	if code != 1 {
		t.Errorf("a match only in a finished run must not count, got exit %d", code)
	}

	_, code = runCLI(t, "logs", "--service", "api", "--wait-for", "never appears", "--timeout", "600ms")
	if code != 1 {
		t.Errorf("a timeout must exit 1, got %d", code)
	}

	_, code = runCLI(t, "logs", "--service", "ghost", "--wait-for", "x", "--timeout", "400ms")
	if code != 1 {
		t.Errorf("a service with no log must exit 1, got %d", code)
	}

	_, code = runCLI(t, "logs", "--service", "api", "--wait-for", "([", "--timeout", "1s")
	if code != 2 {
		t.Errorf("an invalid regexp must exit 2, got %d", code)
	}

	_, code = runCLI(t, "logs", "--all", "--wait-for", "x", "--timeout", "1s")
	if code != 2 {
		t.Errorf("--wait-for with --all must exit 2, got %d", code)
	}

	_, code = runCLI(t, "logs", "--service", "api", "--since", "not-a-time")
	if code != 2 {
		t.Errorf("an unparsable --since must exit 2, got %d", code)
	}
}

func TestRunLogsSinceAndGrepNarrowTheStream(t *testing.T) {
	dir := chdirToTempCompose(t, agentSurfaceCompose)
	logDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "2026-01-01T00:00:00.000Z quiet line\n2026-01-01T00:00:01.000Z ERROR loud line\n"
	if err := os.WriteFile(filepath.Join(logDir, "2026-01-01_000000.ok.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, code := runCLI(t, "logs", "--service", "api", "--grep", "ERROR", "--idle", "1ms"); code != 0 {
		t.Errorf("--grep exit = %d", code)
	}
	if _, code := runCLI(t, "logs", "--service", "api", "--since", "1h", "--idle", "1ms"); code != 0 {
		t.Errorf("--since exit = %d", code)
	}
}

func TestRunCheckpointRefusesTraversalNames(t *testing.T) {
	surfaceWorkspace(t)
	for _, name := range []string{"..", ".hidden", "../escape"} {
		if _, code := runCLI(t, "checkpoint", name); code != 2 {
			t.Errorf("checkpoint %q must exit 2, got %d", name, code)
		}
		if _, code := runCLI(t, "restore", name, "--yes"); code != 2 {
			t.Errorf("restore %q must exit 2, got %d", name, code)
		}
		if _, code := runCLI(t, "checkpoint", "rm", name); code != 2 {
			t.Errorf("checkpoint rm %q must exit 2, got %d", name, code)
		}
	}
}

func TestRunLeasesReleaseRefusesTraversalNames(t *testing.T) {
	surfaceWorkspace(t)
	if _, code := runCLI(t, "leases", "release", "../../etc/passwd"); code != 1 {
		t.Errorf("a traversal lease name must be refused, got %d", code)
	}
}

func TestRunRestoreKeepsCommitsSafeBehindTheGate(t *testing.T) {
	needGit(t)
	dir := surfaceWorkspace(t)
	newRepo(t, dir)

	if _, code := runCLI(t, "checkpoint", "cp-commits"); code != 0 {
		t.Fatal("checkpoint failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("committed later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-qam", "later work")

	out, code := runCLI(t, "restore", "cp-commits", "--yes")
	if code != 0 {
		t.Fatalf("restore exit %d: %s", code, out)
	}
	if !strings.Contains(out, "saved current state as checkpoint") {
		t.Errorf("commits made after the checkpoint must be captured first: %q", out)
	}
}

func TestRunEventsRejectsNothingOnAZeroInterval(t *testing.T) {
	if got := nextEventsPause(time.Time{}); got < minEventsInterval {
		t.Errorf("pause = %s, must not busy-spin", got)
	}
	deadline := time.Now().Add(20 * time.Millisecond)
	if got := nextEventsPause(deadline); got > time.Second {
		t.Errorf("pause = %s, must not overshoot the deadline", got)
	}
}

func TestCheckpointWithDBSkipsNonPostgresServices(t *testing.T) {
	needGit(t)
	dir := chdirToTempCompose(t, `name: dbskip
services:
  api:
    port: 4500
    start:
      - go run .
db_services:
  cache:
    driver: redis
    port: 6399
`)
	newRepo(t, dir)
	if _, err := utils.GetCorgiServices(newRootedCmd()); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, "checkpoint", "cp-db", "--with-db")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "only postgres-family drivers can be snapshotted") {
		t.Errorf("a redis db_service must be reported as skipped: %q", out)
	}

	file, err := readCheckpoint("cp-db")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Databases) != 0 {
		t.Errorf("no snapshot should be recorded, got %+v", file.Databases)
	}
}

func TestRestoreWithDBReportsAMissingSnapshot(t *testing.T) {
	needGit(t)
	dir := surfaceWorkspace(t)
	newRepo(t, dir)

	if _, code := runCLI(t, "checkpoint", "cp-nodb"); code != 0 {
		t.Fatal("checkpoint failed")
	}
	file, err := readCheckpoint("cp-nodb")
	if err != nil {
		t.Fatal(err)
	}
	file.Databases = []checkpointDatabase{{Service: "ghost", Snapshot: "never-made"}}
	if err := writeCheckpoint(file); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, "restore", "cp-nodb", "--yes", "--with-db")
	if code != 0 {
		t.Fatalf("a missing snapshot must not fail the code restore: exit %d, %s", code, out)
	}
	if !strings.Contains(out, "ghost") {
		t.Errorf("the missing snapshot must be named: %q", out)
	}
}
