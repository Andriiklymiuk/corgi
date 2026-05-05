package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestCheckForFlagAndExecuteMakeFlagOff(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("flagX", false, "")
	CheckForFlagAndExecuteMake(cmd, "flagX", "stop")
}

func TestCheckForFlagAndExecuteMakeMissingFlag(t *testing.T) {
	cmd := &cobra.Command{}
	CheckForFlagAndExecuteMake(cmd, "noSuchFlag", "stop")
}

func TestExecuteForEachServiceNoCorgiDir(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/nonexistent-zzz"
	t.Cleanup(func() { CorgiComposePathDir = prev })
	ExecuteForEachService("up")
}

func TestCheckForFlagAndExecuteMakeFlagOn(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// Create a db service dir with a Makefile that has a "stop" target
	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "mydb")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("stop:\n\t@echo noop\n"), 0644); err != nil {
		t.Fatal(err)
	}

	root := &cobra.Command{Use: "root"}
	c := &cobra.Command{Use: "sub"}
	root.AddCommand(c)
	root.Flags().Bool("runOnce", false, "")
	c.Flags().Bool("flagX", false, "")
	c.Flags().Set("flagX", "true")

	CheckForFlagAndExecuteMake(c, "flagX", "stop")
}

func TestExecuteForEachServiceWithDir(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "db1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("stop:\n\t@echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ExecuteForEachService("stop")
}
