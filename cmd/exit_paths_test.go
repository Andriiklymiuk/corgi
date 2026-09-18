package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type exitPanic struct{ code int }

func expectExit(t *testing.T, fn func()) int {
	t.Helper()
	orig := osExit
	osExit = func(c int) { panic(exitPanic{c}) }
	t.Cleanup(func() { osExit = orig })

	code := -1
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			ep, ok := r.(exitPanic)
			if !ok {
				panic(r)
			}
			code = ep.code
		}()
		fn()
	}()
	return code
}

func jsonMode(t *testing.T, on bool) {
	t.Helper()
	prev := utils.JSONOutput
	utils.JSONOutput = on
	t.Cleanup(func() { utils.JSONOutput = prev })
}

func TestFailHelpersExit(t *testing.T) {
	err := errors.New("boom")
	cases := []struct {
		name string
		want int
		fn   func()
	}{
		{"failMemory", 1, func() { failMemory(err) }},
		{"failAutopilot", 1, func() { failAutopilot("autopilot", err) }},
		{"failSuggestHistory", 1, func() { failSuggestHistory(err) }},
		{"failUsage", 2, func() { failUsage("bad usage") }},
		{"failE2E", 1, func() { failE2E("suite failed") }},
		{"emitExecError", 3, func() { emitExecError(utils.ErrUsage, "nope", 3) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expectExit(t, c.fn); got != c.want {
				t.Errorf("exit code = %d, want %d", got, c.want)
			}
		})
	}
}

func TestFailHelpersExitInJSONMode(t *testing.T) {
	jsonMode(t, true)
	err := errors.New("boom")
	cases := []struct {
		name string
		want int
		fn   func()
	}{
		{"failMemory", 1, func() { failMemory(err) }},
		{"failAutopilot", 1, func() { failAutopilot("autopilot", err) }},
		{"failSuggestHistory", 1, func() { failSuggestHistory(err) }},
		{"failUsage", 2, func() { failUsage("bad usage") }},
		{"failE2E", 1, func() { failE2E("suite failed") }},
		{"emitExecError", 4, func() { emitExecError(utils.ErrUsage, "nope", 4) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expectExit(t, c.fn); got != c.want {
				t.Errorf("exit code = %d, want %d", got, c.want)
			}
		})
	}
}

func TestResolvePostgresServiceOrExitOnUnknownService(t *testing.T) {
	dbs := []utils.DatabaseService{{ServiceName: "main", Driver: "postgres"}}
	if got := expectExit(t, func() { resolvePostgresServiceOrExit("nope", dbs) }); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestRemoveSnapshotExitsOnUnresolvablePaths(t *testing.T) {
	if got := expectExit(t, func() { removeSnapshot("svc", "../escape") }); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestComposeLoadFailuresExit(t *testing.T) {
	cases := []struct {
		name string
		fn   func(c *cobra.Command)
	}{
		{"mustLoadCorgiServices", func(c *cobra.Command) { mustLoadCorgiServices(c) }},
		{"loadCorgiForStop", func(c *cobra.Command) { loadCorgiForStop(c) }},
		{"runPs", func(c *cobra.Command) { runPs(c, nil) }},
		{"runOpen", func(c *cobra.Command) { runOpen(c, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := noComposeCmd(t)
			if got := expectExit(t, func() { tc.fn(c) }); got != 1 {
				t.Errorf("exit code = %d, want 1", got)
			}
		})
	}
}

func composeCmd(t *testing.T, body string) *cobra.Command {
	t.Helper()
	chdirToTempCompose(t, body)
	return cmdWithRunFlags()
}

func cmdWithRunFlags() *cobra.Command {
	c := newRootedCmd()
	c.Flags().Bool("strict", false, "")
	c.Flags().Bool("docker", false, "")
	c.Flags().Bool("ensure-deps", false, "")
	c.Flags().String("host", "", "")
	c.Flags().StringSlice("service", nil, "")
	return c
}

func noComposeCmd(t *testing.T) *cobra.Command {
	t.Helper()
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = filepath.Join(dir, "absent")
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	return cmdWithRunFlags()
}

func TestCommandsExitWhenComposeIsMissing(t *testing.T) {
	cases := []struct {
		name string
		want int
		fn   func(c *cobra.Command)
	}{
		{"runValidate", 2, func(c *cobra.Command) { runValidate(c, nil) }},
		{"runDbSnapshot", 1, func(c *cobra.Command) { runDbSnapshot(c, nil) }},
		{"runDbRestore", 1, func(c *cobra.Command) { runDbRestore(c, nil) }},
		{"resolveStatusRows", 1, func(c *cobra.Command) { resolveStatusRows(c) }},
		{"restartSingleService", 1, func(c *cobra.Command) { restartSingleService(c) }},
		{"worktreeList", 1, func(c *cobra.Command) { worktreeListCmd.Run(c, nil) }},
		{"worktreePrune", 1, func(c *cobra.Command) { worktreePruneCmd.Run(c, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := noComposeCmd(t)
			if got := expectExit(t, func() { tc.fn(c) }); got != tc.want {
				t.Errorf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunDbRestoreWithoutNameExits(t *testing.T) {
	c := composeCmd(t, "name: stack\ndb_services:\n  main:\n    driver: postgres\n")
	if got := expectExit(t, func() { runDbRestore(c, nil) }); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestRunValidateExitsOnValidationErrors(t *testing.T) {
	const broken = "name: stack\nservices:\n  api:\n    service_name: api\n    port: 3000\n    depends_on_db:\n      - name: missing\n"
	t.Run("human", func(t *testing.T) {
		c := composeCmd(t, broken)
		if got := expectExit(t, func() { runValidate(c, nil) }); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})
	t.Run("json", func(t *testing.T) {
		jsonMode(t, true)
		c := composeCmd(t, broken)
		if got := expectExit(t, func() { runValidate(c, nil) }); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})
}

func TestWriteMergedLineJSONMode(t *testing.T) {
	jsonMode(t, true)
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	writeMergedLine(out, "api", "2026-01-01T00:00:00.000Z", "hello")
	out.Flush()
	if !strings.Contains(buf.String(), `"api"`) || !strings.Contains(buf.String(), "hello") {
		t.Errorf("json log line = %q", buf.String())
	}
}

func TestWriteMergedLineHumanModeWithAndWithoutTimestamp(t *testing.T) {
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	writeMergedLine(out, "api", "2026-01-01T00:00:00.000Z", "with ts")
	writeMergedLine(out, "web", "", "no ts")
	out.Flush()
	got := buf.String()
	for _, want := range []string{"api", "with ts", "web", "no ts"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
}

const brokenCompose = "name: stack\nservices:\n  api:\n    service_name: api\n    port: 3000\n    depends_on_db:\n      - name: missing\n"

func TestSnapshotNameRejectionExits(t *testing.T) {
	dbs := []utils.DatabaseService{{ServiceName: "main", Driver: "postgres"}}
	if got := expectExit(t, func() { createSnapshot([]string{"../escape"}, dbs) }); got != 1 {
		t.Errorf("createSnapshot exit = %d, want 1", got)
	}
}

func TestRunDbRestoreRefusesNonPostgresService(t *testing.T) {
	c := composeCmd(t, "name: stack\ndb_services:\n  cache:\n    driver: redis\n")
	if got := expectExit(t, func() { runDbRestore(c, []string{"snap", "cache"}) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestResolveStatusRowsExitsWhenFilterMatchesNothing(t *testing.T) {
	c := composeCmd(t, "name: stack\nservices:\n  api:\n    service_name: api\n    port: 3000\n    start_command: echo hi\n")
	if err := c.Flags().Set("service", "nope"); err != nil {
		t.Fatal(err)
	}
	if got := expectExit(t, func() { resolveStatusRows(c) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestMustLoadCorgiServicesExitsInJSONMode(t *testing.T) {
	jsonMode(t, true)
	c := noComposeCmd(t)
	if got := expectExit(t, func() { mustLoadCorgiServices(c) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRunExecExitsOnValidationErrors(t *testing.T) {
	c := composeCmd(t, brokenCompose)
	if got := expectExit(t, func() { runExec(c, []string{"api"}) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestMemoryAddWithoutRequiredFlagsExits(t *testing.T) {
	noComposeCmd(t)
	for _, name := range []string{"type", "name", "desc", "service", "pattern"} {
		if err := memoryAddCmd.Flags().Set(name, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := expectExit(t, func() { memoryAddCmd.Run(memoryAddCmd, nil) }); got != 2 {
		t.Errorf("exit = %d, want 2", got)
	}
}

func TestMemoryLintExitsOnStoreErrors(t *testing.T) {
	write := func(t *testing.T) {
		t.Helper()
		noComposeCmd(t)
		wd, _ := os.Getwd()
		dir := filepath.Join(wd, utils.MemoryDirName, "decisions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("human", func(t *testing.T) {
		write(t)
		if got := expectExit(t, func() { memoryLintCmd.Run(memoryLintCmd, nil) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
	t.Run("json", func(t *testing.T) {
		jsonMode(t, true)
		write(t)
		if got := expectExit(t, func() { memoryLintCmd.Run(memoryLintCmd, nil) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
}

func TestRunCreateNonInteractiveWithoutFlagsExits(t *testing.T) {
	prev := utils.NonInteractive
	utils.NonInteractive = true
	t.Cleanup(func() { utils.NonInteractive = prev })
	prevOpts := createOpts
	createOpts = createFlags{}
	t.Cleanup(func() { createOpts = prevOpts })

	c := noComposeCmd(t)
	if got := expectExit(t, func() { runCreate(c, nil) }); got != 2 {
		t.Errorf("exit = %d, want 2", got)
	}
}

func TestAutopilotStatusExitsOnCorruptState(t *testing.T) {
	dir := chdirToTempCompose(t, "name: stack\n")
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	path := utils.AutopilotStatePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := expectExit(t, func() { autopilotStatusCmd.Run(newRootedCmd(), nil) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRestartSingleServiceExitsWithoutRunState(t *testing.T) {
	c := composeCmd(t, "name: stack\nservices:\n  api:\n    service_name: api\n    port: 3000\n    start_command: echo hi\n")
	prev := restartService
	restartService = "api"
	t.Cleanup(func() { restartService = prev })

	if got := expectExit(t, func() { restartSingleService(c) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestRunStatusOnceExitsWhenAServiceIsDown(t *testing.T) {
	rows := []statusRow{{Label: "api", Port: 1, Kind: "service", URL: "http://localhost:1"}}
	t.Run("human", func(t *testing.T) {
		if got := expectExit(t, func() { runStatusOnce(rows, statusFlags{quiet: true}) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
	t.Run("json", func(t *testing.T) {
		if got := expectExit(t, func() { runStatusOnce(rows, statusFlags{jsonOut: true, quiet: true}) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
}

func TestRunLogsDumpExitsWithNoLogs(t *testing.T) {
	dir := chdirToTempCompose(t, "name: stack\n")
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	prevDump := logsDumpFlag
	logsDumpFlag = filepath.Join(dir, "out")
	t.Cleanup(func() { logsDumpFlag = prevDump })

	if got := expectExit(t, func() { runLogs(newRootedCmd(), nil) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestAutopilotModeExitsWhenStateIsUnwritable(t *testing.T) {
	dir := chdirToTempCompose(t, "name: stack\n")
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := os.WriteFile(filepath.Join(dir, "corgi_services"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	modeCmd := newAutopilotModeCmd("stop", "stop", utils.AutopilotStopped)
	if got := expectExit(t, func() { modeCmd.Run(newRootedCmd(), nil) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestConfigCommandsExitWithoutAHome(t *testing.T) {
	t.Run("path", func(t *testing.T) {
		t.Setenv("HOME", "")
		if got := expectExit(t, func() { configPathCmd.Run(newRootedCmd(), nil) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
	t.Run("show", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".corgi", "config.yml"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := expectExit(t, func() { runConfigShow(newRootedCmd(), nil) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
}

func TestListSnapshotsExitsOnUnreadableDir(t *testing.T) {
	dir := chdirToTempCompose(t, "name: stack\n")
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	snapDir, err := utils.SnapshotsDir("main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(snapDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := expectExit(t, func() { listSnapshots("main") }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestAutopilotHeartbeatExitsWhenStateIsUnwritable(t *testing.T) {
	dir := chdirToTempCompose(t, "name: stack\n")
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := os.WriteFile(filepath.Join(dir, "corgi_services"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := expectExit(t, func() { autopilotHeartbeatCmd.Run(newRootedCmd(), nil) }); got != 1 {
		t.Errorf("exit = %d, want 1", got)
	}
}

func TestSnapshotCommandsRefuseASupervisedStack(t *testing.T) {
	setup := func(t *testing.T) *cobra.Command {
		t.Helper()
		dir := chdirToTempCompose(t, "name: stack\ndb_services:\n  main:\n    driver: postgres\n")
		prev := utils.CorgiComposePathDir
		utils.CorgiComposePathDir = dir
		t.Cleanup(func() { utils.CorgiComposePathDir = prev })

		st := utils.RunState{Services: []utils.RunStateEntry{
			{Name: "api", Kind: "service", PID: 0, Status: "running"},
		}}
		if err := utils.WriteRunState(utils.RunStatePath(dir), st); err != nil {
			t.Fatal(err)
		}
		c := newRootedCmd()
		c.Flags().Bool("docker", false, "")
		return c
	}
	dbs := []utils.DatabaseService{{ServiceName: "main", Driver: "postgres"}}

	t.Run("snapshot", func(t *testing.T) {
		setup(t)
		if got := expectExit(t, func() { createSnapshot([]string{"snap"}, dbs) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
	t.Run("restore", func(t *testing.T) {
		c := setup(t)
		if got := expectExit(t, func() { runDbRestore(c, []string{"snap"}) }); got != 1 {
			t.Errorf("exit = %d, want 1", got)
		}
	})
}
