package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func TestHasDatabaseToRun(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if hasDatabaseToRun(nil) {
			t.Error("want false for nil")
		}
		if hasDatabaseToRun([]utils.DatabaseService{}) {
			t.Error("want false for empty")
		}
	})
	t.Run("all manual returns false", func(t *testing.T) {
		dbs := []utils.DatabaseService{
			{ServiceName: "a", ManualRun: true},
			{ServiceName: "b", ManualRun: true},
		}
		if hasDatabaseToRun(dbs) {
			t.Error("want false when all manual")
		}
	})
	t.Run("at least one non-manual returns true", func(t *testing.T) {
		dbs := []utils.DatabaseService{
			{ServiceName: "a", ManualRun: true},
			{ServiceName: "b", ManualRun: false},
		}
		if !hasDatabaseToRun(dbs) {
			t.Error("want true")
		}
	})
}

func TestOmitServiceCmd(t *testing.T) {
	t.Cleanup(func() { omitItems = nil })
	omitItems = []string{"beforeStart", "afterStart"}
	if !omitServiceCmd("beforeStart") {
		t.Error("want true")
	}
	if !omitServiceCmd("afterStart") {
		t.Error("want true")
	}
	if omitServiceCmd("start") {
		t.Error("want false")
	}
}

func TestGetServiceEnv(t *testing.T) {
	t.Run("nil AutoSourceEnv returns EnvPath", func(t *testing.T) {
		got := getServiceEnv(utils.Service{EnvPath: ".env.local"})
		if got != ".env.local" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("AutoSourceEnv true returns EnvPath", func(t *testing.T) {
		on := true
		got := getServiceEnv(utils.Service{AutoSourceEnv: &on, EnvPath: ".env"})
		if got != ".env" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("AutoSourceEnv false returns sentinel", func(t *testing.T) {
		off := false
		got := getServiceEnv(utils.Service{AutoSourceEnv: &off, EnvPath: ".env"})
		if got != utils.SkipAutoSourceEnv {
			t.Errorf("got %q want SkipAutoSourceEnv", got)
		}
	})
}

func TestShouldSkipManualRun(t *testing.T) {
	t.Cleanup(func() { utils.ServicesItemsFromFlag = nil })

	t.Run("non-manual not skipped", func(t *testing.T) {
		utils.ServicesItemsFromFlag = nil
		if shouldSkipManualRun(utils.Service{ServiceName: "x"}) {
			t.Error("want false")
		}
	})

	t.Run("manual + no flag = skip", func(t *testing.T) {
		utils.ServicesItemsFromFlag = nil
		if !shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want true")
		}
	})

	t.Run("manual but in flag = run", func(t *testing.T) {
		utils.ServicesItemsFromFlag = []string{"x"}
		if shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want false (included in flag)")
		}
	})

	t.Run("manual and flag set but not included = skip", func(t *testing.T) {
		utils.ServicesItemsFromFlag = []string{"y"}
		if !shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want true")
		}
	})
}

func TestStartDatabaseIfNeededManualRunNoop(t *testing.T) {
	startDatabaseIfNeeded(utils.DatabaseService{ManualRun: true, ServiceName: "x"})
}

func TestStartAllServicesNoStartCmd(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("tunnel", false, "")
	startAllServices(corgi, cmd)
}

func TestInstallSignalHandlerStopIsIdempotent(t *testing.T) {
	stop := installSignalHandler(&cobra.Command{})
	stop()
	stop()
}

func TestInstallSignalHandlerNoLeak(t *testing.T) {
	for i := 0; i < 5; i++ {
		stop := installSignalHandler(&cobra.Command{})
		stop()
	}
}

func TestRunServiceSkipsManual(t *testing.T) {
	t.Cleanup(func() { utils.ServicesItemsFromFlag = nil })
	utils.ServicesItemsFromFlag = nil

	cmd := &cobra.Command{}
	cmd.Flags().Bool("pull", false, "")

	var wg sync.WaitGroup
	wg.Add(1)
	runService(utils.Service{ServiceName: "manual", ManualRun: true}, cmd, &wg)
	wg.Wait()
}

func TestSetupComposeWatcherNoWatch(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-watch", true, "")
	w, err := setupComposeWatcher(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Errorf("expected nil watcher")
	}
}

func TestSetupComposeWatcherFlagMissing(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := setupComposeWatcher(cmd)
	if err == nil {
		t.Error("expected err when flag missing")
	}
}

func TestRunServicePullIfRequestedNoPull(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("pull", false, "")
	runServicePullIfRequested(cmd, utils.Service{ServiceName: "x"})
}

func TestRunServicePullIfRequestedFlagMissing(t *testing.T) {
	cmd := &cobra.Command{}
	runServicePullIfRequested(cmd, utils.Service{ServiceName: "x"})
}

func TestStartServiceProcessNoStart(t *testing.T) {
	startServiceProcess(utils.Service{ServiceName: "x"})
}

func TestRunDatabaseServicesEmpty(t *testing.T) {
	cmd := &cobra.Command{}
	runDatabaseServices(cmd, nil)
}

func TestRunDatabaseServicesAllManual(t *testing.T) {
	cmd := &cobra.Command{}
	runDatabaseServices(cmd, []utils.DatabaseService{
		{ServiceName: "x", ManualRun: true},
	})
}

func newTestComposeCommand() (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "corgi"}
	c := &cobra.Command{Use: "sub"}
	root.AddCommand(c)
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		root.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		root.Flags().Bool(f, false, "")
	}
	c.Flags().Bool("global", false, "")
	return root, c
}

func TestGetCorgiServicesViaCobra(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	corgi, err := utils.GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	if corgi.Name != "test" {
		t.Errorf("name = %q", corgi.Name)
	}
}

func TestCleanupNoDbsNoServices(t *testing.T) {
	cleanup(&utils.CorgiCompose{})
}

func TestCleanupSkipsAfterStartNil(t *testing.T) {
	cleanup(&utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "x", AfterStart: nil},
		},
	})
}

func TestHandleComposeWriteEventErrorReadingNew(t *testing.T) {
	// Without compose file, GetCorgiServices fails → handleComposeWriteEvent returns true (stop)
	cmd := &cobra.Command{}
	got := handleComposeWriteEvent(nil, cmd, "x")
	if !got {
		t.Errorf("expected true on error")
	}
}

func TestCleanupWithAfterStart(t *testing.T) {
	dir := t.TempDir()
	cleanup(&utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "x", AfterStart: []string{"echo after"}, AbsolutePath: dir},
		},
	})
}

func TestCleanupWithGlobalAfterStart(t *testing.T) {
	cleanup(&utils.CorgiCompose{
		AfterStart: []string{"echo global-after"},
	})
}

func TestCleanupAfterStartActuallyRuns(t *testing.T) {
	prev := utils.ProcessHandles
	utils.ProcessHandles = nil
	t.Cleanup(func() { utils.ProcessHandles = prev })

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	cleanup(&utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "x", AfterStart: []string{"touch " + marker}, AbsolutePath: dir},
		},
	})
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("afterStart did not run: %v", err)
	}
	if len(utils.ProcessHandles) != 0 {
		t.Errorf("afterStart must not be tracked, got %d handles", len(utils.ProcessHandles))
	}
}

func TestCleanupAfterStartTimeoutDoesNotBlock(t *testing.T) {
	prev := utils.ProcessHandles
	utils.ProcessHandles = nil
	t.Cleanup(func() { utils.ProcessHandles = prev })

	prevTimeout := utils.AfterStartTimeout
	utils.AfterStartTimeout = 200 * time.Millisecond
	t.Cleanup(func() { utils.AfterStartTimeout = prevTimeout })

	start := time.Now()
	cleanup(&utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "x", AfterStart: []string{"sleep 30"}, AbsolutePath: t.TempDir()},
		},
	})
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("cleanup blocked past timeout, took %s", elapsed)
	}
}

func TestRunService_BailsOnShutdownBeforeSpawning(t *testing.T) {
	utils.ResetShutdownForTests()
	t.Cleanup(utils.ResetShutdownForTests)
	utils.RequestShutdown()

	prev := utils.ProcessHandles
	utils.ProcessHandles = nil
	t.Cleanup(func() { utils.ProcessHandles = prev })

	cmd := &cobra.Command{}
	cmd.Flags().Bool("pull", false, "")

	var wg sync.WaitGroup
	wg.Add(1)
	runService(utils.Service{
		ServiceName:  "x",
		BeforeStart:  []string{"touch should-not-run"},
		Start:        []string{"sleep 30"},
		AbsolutePath: t.TempDir(),
	}, cmd, &wg)
	wg.Wait()

	if len(utils.ProcessHandles) != 0 {
		t.Errorf("runService must not spawn processes after shutdown, got %d handles", len(utils.ProcessHandles))
	}
}

func TestExitInProgressBlocksReentry(t *testing.T) {
	t.Cleanup(func() { exitInProgress.Store(false) })

	exitInProgress.Store(false)
	if !exitInProgress.CompareAndSwap(false, true) {
		t.Fatal("first CAS should claim the flag")
	}
	if exitInProgress.CompareAndSwap(false, true) {
		t.Fatal("second CAS must not succeed while exit in progress")
	}
}

func TestExitInProgressResetsForRetry(t *testing.T) {
	t.Cleanup(func() { exitInProgress.Store(false) })

	exitInProgress.Store(false)
	exitInProgress.CompareAndSwap(false, true)
	// Simulate cleanup-setup error path resetting the flag.
	exitInProgress.Store(false)
	if !exitInProgress.CompareAndSwap(false, true) {
		t.Fatal("after reset, next signal must be able to claim the flag")
	}
}

func TestRunServiceWithStartCommands(t *testing.T) {
	t.Cleanup(func() { utils.ServicesItemsFromFlag = nil })
	utils.ServicesItemsFromFlag = nil

	cmd := &cobra.Command{}
	cmd.Flags().Bool("pull", false, "")
	dir := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(1)
	runService(utils.Service{
		ServiceName:  "x",
		Start:        []string{"echo hi"},
		AbsolutePath: dir,
	}, cmd, &wg)
	wg.Wait()
}

func TestRunServiceWithBeforeStart(t *testing.T) {
	t.Cleanup(func() { utils.ServicesItemsFromFlag = nil })
	utils.ServicesItemsFromFlag = nil

	cmd := &cobra.Command{}
	cmd.Flags().Bool("pull", false, "")
	dir := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(1)
	runService(utils.Service{
		ServiceName:  "x",
		BeforeStart:  []string{"echo before"},
		AbsolutePath: dir,
	}, cmd, &wg)
	wg.Wait()
}

func TestHandleComposeWriteEventSameContent(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: same\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	corgi, err := utils.GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	utils.CorgiComposeFileContent = corgi

	got := handleComposeWriteEvent(nil, c, "corgi-compose.yml")
	if got {
		t.Error("expected false when content is same")
	}
}

func TestSetupComposeWatcherNoWatchFalse(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-watch", false, "")
	w, err := setupComposeWatcher(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Error("expected watcher when no-watch=false")
	}
	w.Close()
}

func TestStartServiceProcessDockerPort(t *testing.T) {
	// docker runner with port — tries ExecuteServiceCommandRun("make up") which will fail gracefully
	startServiceProcess(utils.Service{
		ServiceName: "svc",
		Runner:      utils.Runner{Name: "docker"},
		Port:        8080,
	})
}

func TestStartServiceProcessStartCmds(t *testing.T) {
	dir := t.TempDir()
	startServiceProcess(utils.Service{
		ServiceName:  "svc",
		Start:        []string{"echo hi"},
		AbsolutePath: dir,
	})
}

func TestRunDatabaseServicesWithNonManual(t *testing.T) {
	// hasDatabaseToRun=true, DockerInit must NOT launch Docker.app in the
	// test environment — pre-set shutdown so startDockerAndWait bails
	// before invoking StartDocker.
	utils.ResetShutdownForTests()
	t.Cleanup(utils.ResetShutdownForTests)
	utils.RequestShutdown()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("seed", false, "")
	runDatabaseServices(cmd, []utils.DatabaseService{
		{ServiceName: "pg", Driver: "postgres", Port: 5432, ManualRun: false},
	})
}

func TestStartDatabaseIfNeededNotRunning(t *testing.T) {
	// ManualRun=false, but IsServiceRunning will fail (no docker) → prints error, continues
	startDatabaseIfNeeded(utils.DatabaseService{
		ServiceName: "pg",
		Driver:      "postgres",
		ManualRun:   false,
	})
}

func TestWatchCorgiComposeNoWatch(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-watch", true, "")
	cmd.Flags().Set("no-watch", "true")
	// setupComposeWatcher returns nil when no-watch=true
	w, err := setupComposeWatcher(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		w.Close()
		t.Error("expected nil watcher when no-watch=true")
	}
}

func TestHandleComposeWriteEventReadError(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	_, c := newTestComposeCommand()
	got := handleComposeWriteEvent(nil, c, "corgi-compose.yml")
	if !got {
		t.Error("expected true (stop watching) when file missing")
	}
}

func TestHandleComposeWriteEventNoChange(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: same\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	corgi, err := utils.GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	utils.CorgiComposeFileContent = corgi

	got := handleComposeWriteEvent(nil, c, "corgi-compose.yml")
	if got {
		t.Error("expected false when content unchanged")
	}
}

func TestHandleExistingServiceDirWithCloneAndBranch(t *testing.T) {
	handleExistingServiceDir(utils.Service{
		ServiceName:  "x",
		CloneFrom:    "https://invalid.example.invalid/repo.git",
		Branch:       "main",
		AbsolutePath: t.TempDir(),
	})
}

func TestRunServicePullIfRequestedPullTrue(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("pull", true, "")
	if err := c.Flags().Set("pull", "true"); err != nil {
		t.Fatal(err)
	}
	runServicePullIfRequested(c, utils.Service{ServiceName: "svc", AbsolutePath: t.TempDir()})
}

func TestHandleComposeWriteEventContentChanged(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	corgi, err := utils.GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	utils.CorgiComposeFileContent = corgi

	if err := os.WriteFile(yml, []byte("name: v2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	got := handleComposeWriteEvent(w, c, yml)
	if got {
		t.Error("expected false when content changed (not a read error)")
	}
}
