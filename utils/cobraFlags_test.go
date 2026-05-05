package utils

import (
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
