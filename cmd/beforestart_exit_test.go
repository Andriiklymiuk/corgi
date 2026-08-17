package cmd

import (
	"errors"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// A run whose setup failed used to report success, so a script could not tell
// a healthy stack from one missing a service.
func TestReportBeforeStartFailuresExitsNonZero(t *testing.T) {
	utils.ResetBeforeStartFailures()
	t.Cleanup(utils.ResetBeforeStartFailures)
	utils.RecordBeforeStartFailure("api", errors.New("exit status 127"))

	code, msg := captureExit(t, reportBeforeStartFailures)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !contains(msg, "api") {
		t.Errorf("the failing service must be named, got %q", msg)
	}
}

func TestReportBeforeStartFailuresIsSilentOnASuccessfulRun(t *testing.T) {
	utils.ResetBeforeStartFailures()
	t.Cleanup(utils.ResetBeforeStartFailures)

	code, _ := captureExit(t, reportBeforeStartFailures)
	if code != 0 {
		t.Errorf("a clean run must not exit non-zero, got %d", code)
	}
}

// The compose watcher and corgi restart re-enter run in the same process.
// Exiting there would tear down a session the developer is still using.
func TestReportBeforeStartFailuresDoesNotExitWhileReloading(t *testing.T) {
	utils.ResetBeforeStartFailures()
	t.Cleanup(utils.ResetBeforeStartFailures)
	utils.RecordBeforeStartFailure("api", errors.New("boom"))

	runReloading.Store(true)
	t.Cleanup(func() { runReloading.Store(false) })

	code, _ := captureExit(t, reportBeforeStartFailures)
	if code != 0 {
		t.Errorf("a hot reload must survive a failed beforeStart, got exit %d", code)
	}
}

// captureExit swaps the process exit for a recorder, so a path that ends in
// os.Exit can be asserted instead of killing the test binary.
func captureExit(t *testing.T, fn func()) (code int, stderr string) {
	t.Helper()
	orig := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = orig })
	stderr = captureStderr(t, fn)
	return code, stderr
}
