package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
