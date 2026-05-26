package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func tierTestCmd(yes bool) *cobra.Command {
	c := &cobra.Command{Use: "run"}
	c.Flags().Bool("yes", yes, "")
	return c
}

func TestConfirmTier_NoActiveTier(t *testing.T) {
	defer func() { utils.ActiveTierName = "" }()
	utils.ActiveTierName = ""
	if err := confirmTier(tierTestCmd(false), &utils.CorgiCompose{}); err != nil {
		t.Fatalf("no tier should not error: %v", err)
	}
}

func TestConfirmTier_NonConfirmTierPasses(t *testing.T) {
	defer func() { utils.ActiveTierName = "" }()
	utils.ActiveTierName = "staging"
	corgi := &utils.CorgiCompose{EnvTiers: map[string]utils.EnvTier{"staging": {Dir: "env/staging"}}}
	if err := confirmTier(tierTestCmd(false), corgi); err != nil {
		t.Fatalf("non-confirm tier should pass: %v", err)
	}
}

func TestConfirmTier_ConfirmTierWithYes(t *testing.T) {
	defer func() { utils.ActiveTierName = "" }()
	utils.ActiveTierName = "prod"
	corgi := &utils.CorgiCompose{EnvTiers: map[string]utils.EnvTier{"prod": {Confirm: true}}}
	if err := confirmTier(tierTestCmd(true), corgi); err != nil {
		t.Fatalf("--yes should bypass confirm: %v", err)
	}
}

func TestConfirmTier_ConfirmTierNonInteractiveNoYes(t *testing.T) {
	defer func() { utils.ActiveTierName = ""; utils.NonInteractive = false }()
	utils.ActiveTierName = "prod"
	utils.NonInteractive = true
	corgi := &utils.CorgiCompose{EnvTiers: map[string]utils.EnvTier{"prod": {Confirm: true}}}
	if err := confirmTier(tierTestCmd(false), corgi); err == nil {
		t.Fatal("non-interactive confirm tier without --yes should error")
	}
}
