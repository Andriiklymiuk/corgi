package cmd

import (
	"errors"
	"testing"

	"andriiklymiuk/corgi/utils"
)

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

func captureExit(t *testing.T, fn func()) (code int, stderr string) {
	t.Helper()
	orig := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = orig })
	stderr = captureStderr(t, fn)
	return code, stderr
}
