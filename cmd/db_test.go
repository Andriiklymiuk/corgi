package cmd

import (
	"andriiklymiuk/corgi/utils"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCheckForSeedAllFlagFalse(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("seedAll", false, "")
	checkForSeedAllFlag(c, nil)
}

func TestCheckForSeedAllFlagMissing(t *testing.T) {
	c := &cobra.Command{}
	checkForSeedAllFlag(c, nil)
}

func TestSeedDbMissingDump(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	dir := filepath.Join(utils.CorgiComposePathDir, utils.RootDbServicesFolder, "db1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	err := SeedDb("db1")
	if err == nil || !strings.Contains(err.Error(), "dump file doesn't exist") {
		t.Errorf("expected dump-not-found error, got %v", err)
	}
}

func TestSeedDbReadDirError(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	err := SeedDb("nonexistent-svc")
	if err == nil {
		t.Error("expected err")
	}
}

func TestDumpAndSeedDbMissingSource(t *testing.T) {
	err := DumpAndSeedDb(utils.DatabaseService{
		ServiceName:      "db1",
		Driver:           "postgres",
		SeedFromFilePath: "/nonexistent/file.sql",
	})
	if err == nil {
		t.Error("expected err")
	}
}

func TestSeedAllDatabasesEmpty(t *testing.T) {
	SeedAllDatabases(nil)
}

func TestErrMakeCommandFailedConst(t *testing.T) {
	if errMakeCommandFailed != "Make command failed" {
		t.Errorf("got %q", errMakeCommandFailed)
	}
}

func TestDbShellCmd_Registered(t *testing.T) {
	// Verify the shell subcommand is registered under db.
	found := false
	for _, sub := range dbCmd.Commands() {
		if sub.Use == "shell [service-name]" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected `shell` subcommand under `corgi db`")
	}
}

func TestRequireServiceForDBShell(t *testing.T) {
	err := requireServiceForDBShell("", true, []string{"postgres", "redis"})
	if err == nil {
		t.Fatal("expected error when no service under non-interactive")
	}
	if !strings.Contains(err.Error(), "postgres") || !strings.Contains(err.Error(), "service") {
		t.Errorf("error should mention service and list available, got %q", err.Error())
	}
	if requireServiceForDBShell("postgres", true, []string{"postgres"}) != nil {
		t.Error("explicit service should pass")
	}
	if requireServiceForDBShell("", false, []string{"postgres"}) != nil {
		t.Error("interactive mode should allow empty service")
	}
}

func TestWaitForDbsReady_AllReady(t *testing.T) {
	dbs := []utils.DatabaseService{
		{ServiceName: "pg", Port: 5432},
		{ServiceName: "redis", Port: 6379},
	}
	var probed []string
	ready := func(_ context.Context, db utils.DatabaseService) error {
		probed = append(probed, db.ServiceName)
		return nil
	}
	if err := waitForDbsReady(context.Background(), dbs, ready); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(probed) != 2 {
		t.Fatalf("want both probed, got %v", probed)
	}
}

func TestWaitForDbsReady_SkipsPortless(t *testing.T) {
	dbs := []utils.DatabaseService{{ServiceName: "noport", Port: 0}}
	called := false
	ready := func(_ context.Context, _ utils.DatabaseService) error { called = true; return nil }
	if err := waitForDbsReady(context.Background(), dbs, ready); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("port==0 db should be skipped")
	}
}

func TestWaitForDbsReady_WrapsError(t *testing.T) {
	dbs := []utils.DatabaseService{{ServiceName: "pg", Port: 5432}}
	ready := func(_ context.Context, _ utils.DatabaseService) error { return errors.New("timeout") }
	err := waitForDbsReady(context.Background(), dbs, ready)
	if err == nil || !strings.Contains(err.Error(), "pg") {
		t.Fatalf("want error naming pg, got %v", err)
	}
}
