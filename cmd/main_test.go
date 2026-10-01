package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sendgate"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sig {
		}
	}()
	utils.SilenceNotificationsForTests()
	notifyGate = func() *sendgate.Gate { return sendgate.InMemory() }
	os.Exit(m.Run())
}
