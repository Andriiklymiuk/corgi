package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContinueOnPutsTheRetryModeWhereAppsReadIt(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd is macOS")
	}
	var calls []string
	orig := launchctlEnv
	launchctlEnv = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte("1\n"), nil
	}
	t.Cleanup(func() { launchctlEnv = orig })
	setWaitOutLimitsEnv(true)
	want := []string{"setenv CLAUDE_CODE_RETRY_WATCHDOG 1", "setenv CLAUDE_CODE_RETRY_WATCHDOG_USAGE_LIMIT_RECHECK_MS 300000", "setenv CLAUDE_CODE_RETRY_WATCHDOG_MAX_USAGE_LIMIT_WAIT_MS 21600000"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", calls)
	}
	calls = nil
	setWaitOutLimitsEnv(false)
	if len(calls) != 3 || !strings.HasPrefix(calls[0], "unsetenv CLAUDE_CODE_RETRY_WATCHDOG") {
		t.Fatalf("off unsets: %v", calls)
	}
	if !waitOutLimitsSet() {
		t.Fatal("getenv 1 means set")
	}
	launchctlEnv = func(args ...string) ([]byte, error) { return nil, errors.New("no launchd") }
	if waitOutLimitsSet() {
		t.Fatal("an error is not set")
	}
}

func TestTheSetupCheckSaysWhoWaitsOutLimits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	if c := checkWaitOutLimits(dir); c.OK != true || !strings.Contains(c.Detail, "off") || c.Fix == "" {
		t.Fatalf("off: %+v", c)
	}
	path := agentUserConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("autoContinue: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := launchctlEnv
	launchctlEnv = func(args ...string) ([]byte, error) { return []byte("1"), nil }
	t.Cleanup(func() { launchctlEnv = orig })
	c := checkWaitOutLimits(dir)
	if !c.OK || c.Fix != "" {
		t.Fatalf("on and set: %+v", c)
	}
	if runtime.GOOS == "darwin" {
		launchctlEnv = func(args ...string) ([]byte, error) { return []byte(""), nil }
		if c := checkWaitOutLimits(dir); c.Fix == "" || !strings.Contains(c.Fix, "continue on") {
			t.Fatalf("on but not set names the fix: %+v", c)
		}
	}
}
