package cmd

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A daemon the record forgot is still a daemon. These tests swap the process
// scan so nothing here can signal the machine's real one.

func withStrays(t *testing.T, pids ...int) {
	t.Helper()
	orig := otherServers
	otherServers = func(int) []int { return pids }
	t.Cleanup(func() { otherServers = orig })
}

func TestStopStrayServersEndsWhatTheRecordForgot(t *testing.T) {
	sleeper := exec.Command("sleep", "60")
	if err := sleeper.Start(); err != nil {
		t.Skip("no sleep binary:", err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _, _ = sleeper.Process.Wait() })
	done := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(done) }()
	orig := otherServers
	t.Cleanup(func() { otherServers = orig })
	otherServers = func(int) []int {
		// Listed until it has exited; the stop's wait loop polls this.
		select {
		case <-done:
			return nil
		default:
			return []int{sleeper.Process.Pid}
		}
	}

	if n := stopStrayServers(); n != 1 {
		t.Fatalf("stopStrayServers() = %d, want 1", n)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stray should have been stopped")
	}
}

func TestStopStrayServersWithNothingToStop(t *testing.T) {
	withStrays(t)
	if n := stopStrayServers(); n != 0 {
		t.Errorf("stopStrayServers() = %d, want 0", n)
	}
}

func TestWarnStrayServersNamesOnlyTheUnrecorded(t *testing.T) {
	withStrays(t, 10, 20)

	out := captureStdout(t, func() { warnStrayServers(10) })

	if !strings.Contains(out, "pid 20") || strings.Contains(out, "10") {
		t.Errorf("warning = %q, want only the unrecorded pid 20", out)
	}
	if quiet := captureStdout(t, func() { withStrays(t); warnStrayServers(0) }); quiet != "" {
		t.Errorf("no strays must print nothing, got %q", quiet)
	}
}

func TestEnsureDaemonRefusesToStartBesideAStray(t *testing.T) {
	withStrays(t, 77)

	info, err := ensureDaemon(t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "77") || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("ensureDaemon() = %+v, %v; want a refusal naming pid 77 and the way out", info, err)
	}
}
