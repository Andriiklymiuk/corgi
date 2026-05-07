package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// TestMain swallows process-wide signals (SIGHUP/SIGINT/SIGTERM) so production
// code paths that call utils.SendRestart() / SendInterrupt() do not kill the
// test binary. Without this, handleComposeWriteEvent → SendRestart fires
// SIGHUP at our own pid and the default Go signal handler exits the process.
func TestMain(m *testing.M) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sig {
		}
	}()
	os.Exit(m.Run())
}
