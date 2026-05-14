package cmd

import (
	"andriiklymiuk/corgi/utils"
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
