//go:build windows

package daemon

import "os"

// syscallZero is unused on Windows; see processAliveOS.
var syscallZero os.Signal = nil

// processAliveOS reports whether pid is a live process.
//
// Windows has no signal 0, and os.Process.Signal(nil) returns EWINDOWS for a
// live process — so probing by signal reports every daemon as dead, which made
// ReadInfo delete daemon.json and defeated the already-running guard.
// os.FindProcess opens a handle on Windows and fails for a pid that is gone,
// which is the check we actually want.
func processAliveOS(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}

// Windows has no SIGUSR1; the command tick alone drains the spool (≤5s later).
func nudgeProcess(int) error { return nil }

// notifyNudge has nothing to subscribe to on Windows (no SIGUSR1), so it
// installs no handler and returns a stop function with nothing to undo.
func notifyNudge(chan<- struct{}) func() {
	return func() {
		// No signal handler was installed, so there is nothing to tear down.
	}
}
