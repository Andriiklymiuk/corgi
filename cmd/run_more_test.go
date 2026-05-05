package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

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
