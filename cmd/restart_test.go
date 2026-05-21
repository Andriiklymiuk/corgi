package cmd

import "testing"

func TestRestartCmdRegistered(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"restart"})
	if err != nil || c.Name() != "restart" {
		t.Fatalf("restart command not registered: %v", err)
	}
	if c.Flags().Lookup("service") == nil {
		t.Error("restart should have --service flag")
	}
}

func TestRestartCmdHasRunFlags(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"restart"})
	if err != nil {
		t.Fatalf("restart command not found: %v", err)
	}
	for _, name := range []string{"detach", "force", "host"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("restart should have --%s flag (runRun reads it)", name)
		}
	}
}
