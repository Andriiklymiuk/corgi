package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func newFlagCmd() *cobra.Command {
	c := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
	c.Flags().Bool("json", false, "")
	c.Flags().Bool("interactive", false, "")
	return c
}

func TestApplyGlobalFlagsJSON(t *testing.T) {
	utils.JSONOutput = false
	c := newFlagCmd()
	c.ParseFlags([]string{"--json"})
	applyGlobalFlags(c)
	if !utils.JSONOutput {
		t.Errorf("JSONOutput not set by --json")
	}
}

func TestApplyGlobalFlagsInteractive(t *testing.T) {
	utils.NonInteractive = true
	c := newFlagCmd()
	c.ParseFlags([]string{"--interactive"})
	applyGlobalFlags(c)
	if utils.NonInteractive {
		t.Errorf("--interactive should clear NonInteractive")
	}
}
