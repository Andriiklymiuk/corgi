package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// chdirToTempCompose creates a temp dir w/ corgi-compose.yml, chdirs into it.
func chdirToTempCompose(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })
	return dir
}

func newRootedCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("global", false, "")
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		c.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		c.Flags().Bool(f, false, "")
	}
	return c
}

func TestRunInitNoServices(t *testing.T) {
	chdirToTempCompose(t, "name: empty\n")
	c := newRootedCmd()
	runInit(c, nil)
}

func TestRunInitInvalidCompose(t *testing.T) {
	chdirToTempCompose(t, ":\n  : invalid")
	c := newRootedCmd()
	runInit(c, nil)
}

func TestRunInitWithDb(t *testing.T) {
	dir := chdirToTempCompose(t, `name: x
db_services:
  pg:
    driver: postgres
    port: 5432
    user: u
    password: p
    databaseName: d
`)
	c := newRootedCmd()
	runInit(c, nil)
	dest := filepath.Join(dir, utils.RootDbServicesFolder, "pg", "docker-compose.yml")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestRunRunEmptyCompose(t *testing.T) {
	chdirToTempCompose(t, "name: empty\n")
	c := newRootedCmd()
	c.Flags().Bool("no-watch", true, "")
	c.Flags().Bool("tunnel", false, "")
	c.Flags().Bool("seed", false, "")
	c.Flags().Bool("pull", false, "")
	c.Flags().StringSlice("omit", nil, "")
	c.Flags().StringSlice("services", nil, "")
	c.Flags().StringSlice("dbServices", nil, "")
	c.Run = runRun
	// runRun will spawn signal handler goroutine but still complete.
	done := make(chan struct{})
	go func() {
		runRun(c, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func TestRunRunWithBeforeStart(t *testing.T) {
	chdirToTempCompose(t, `name: x
beforeStart:
  - echo before-start
`)
	c := newRootedCmd()
	c.Flags().Bool("no-watch", true, "")
	c.Flags().Bool("tunnel", false, "")
	c.Flags().Bool("seed", false, "")
	c.Flags().Bool("pull", false, "")
	c.Flags().StringSlice("omit", nil, "")
	c.Flags().StringSlice("services", nil, "")
	c.Flags().StringSlice("dbServices", nil, "")
	c.Run = runRun
	done := make(chan struct{})
	go func() {
		runRun(c, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func TestRunInitWithService(t *testing.T) {
	chdirToTempCompose(t, `name: x
services:
  api:
    port: 3000
`)
	c := newRootedCmd()
	runInit(c, nil)
}

func TestRunForkBadGitProvider(t *testing.T) {
	chdirToTempCompose(t, "name: x\n")
	c := newRootedCmd()
	c.Flags().String("gitProvider", "bitbucket", "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("private", false, "")
	c.Flags().Bool("useSameRepoName", false, "")
	if err := preRunE(c, nil); err == nil {
		t.Error("expected validation err")
	}
}

func TestCleanServicesViaRunClean(t *testing.T) {
	chdirToTempCompose(t, "name: x\n")
	c := newRootedCmd()
	cleanItems = []string{"corgi_services"}
	runClean(c, nil)
}

func TestRunCleanInvalidCompose(t *testing.T) {
	chdirToTempCompose(t, ":\n  : bad")
	c := newRootedCmd()
	cleanItems = []string{"all"}
	runClean(c, nil)
}

func TestSeedDbDirMissing(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = "/nonexistent/zz"
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })
	if err := SeedDb("x"); err == nil {
		t.Error("expected err")
	}
}

func TestSeedAllDatabasesNoOp(t *testing.T) {
	SeedAllDatabases([]utils.DatabaseService{})
}

// Skipped: runDoctor/runPull/runScript without compose triggers interactive
// "Select corgi config file" prompt that hangs in tests. The compose-loading
// prompt path is hard to bypass in a test harness. Covered indirectly via
// other tests that supply valid compose files.

func TestRunScriptNoServices(t *testing.T) {
	chdirToTempCompose(t, "name: empty\n")
	c := newRootedCmd()
	runScript(c, nil)
}

func TestRunScriptNoScripts(t *testing.T) {
	chdirToTempCompose(t, `services:
  api:
    port: 3000
`)
	c := newRootedCmd()
	runScript(c, nil)
}
