package cmd

import (
	"strings"
	"testing"
)

func TestUpgradedIsQuietOnAMachineWithNoLoginService(t *testing.T) {
	ticketHome(t)
	out := captureStdout(t, func() { agentUpgradedCmd.Run(agentUpgradedCmd, nil) })
	if strings.TrimSpace(out) != "corgi "+APP_VERSION {
		t.Fatalf("out = %q", out)
	}
	found := false
	for _, c := range agentCmd.Commands() {
		found = found || c.Name() == "upgraded"
	}
	if !found {
		t.Fatalf("corgi agent upgraded is not registered")
	}
}
