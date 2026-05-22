package cmd

import (
	"strings"
	"testing"
)

func TestRestartUnsupportedMessage(t *testing.T) {
	msg := restartUnsupportedMessage("api")
	if !strings.Contains(msg, "not supported yet") {
		t.Errorf("expected unsupported notice, got %q", msg)
	}
	if !strings.Contains(msg, "corgi stop --service api") {
		t.Errorf("expected stop hint with service name, got %q", msg)
	}
	if !strings.Contains(msg, "corgi run --detach") {
		t.Errorf("expected run --detach hint, got %q", msg)
	}
}

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
