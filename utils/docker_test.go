package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsDockerContextValid(t *testing.T) {
	for _, valid := range []string{"default", "orbctl", "colima", "docker-linux"} {
		if !isDockerContextValid(valid) {
			t.Errorf("expected %q to be valid", valid)
		}
	}
	if isDockerContextValid("") {
		t.Error("empty should be invalid")
	}
	if isDockerContextValid("nope") {
		t.Error("unknown should be invalid")
	}
}

func TestDockerContextConfigsHasAll(t *testing.T) {
	for _, key := range []string{"default", "orbctl", "colima", "docker-linux"} {
		if _, ok := DockerContextConfigs[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestTryDockerContextStartInvalid(t *testing.T) {
	if tryDockerContextStart("") {
		t.Error("empty should not start")
	}
	if tryDockerContextStart("default") {
		t.Error("default should not trigger context start (it's not a context override)")
	}
	if tryDockerContextStart("docker-linux") {
		t.Error("docker-linux should not trigger context start")
	}
	if tryDockerContextStart("nope") {
		t.Error("invalid should not start")
	}
}

func TestErrDockerNotOpenedConst(t *testing.T) {
	if errDockerNotOpened != "docker not opened" {
		t.Errorf("got %q", errDockerNotOpened)
	}
}

func TestDockerContextConfigsCanCallStart(t *testing.T) {
	cfg := DockerContextConfigs["orbctl"]
	if cfg.Name != "orbctl" {
		t.Errorf("name = %q", cfg.Name)
	}
}

func TestIsDockerRunning(t *testing.T) {
	_ = IsDockerRunning()
}

func TestIsPortListeningClosed(t *testing.T) {
	if IsPortListening(1) {
		t.Error("port 1 should not be listening")
	}
}

func TestPortOwnerNoListener(t *testing.T) {
	got := PortOwner(1)
	_ = got
}

func TestCheckDockerStatusRunsCmd(t *testing.T) {
	err := CheckDockerStatus()
	_ = err
}

func TestIsServiceRunningFalseForFakeContainer(t *testing.T) {
	running, err := IsServiceRunning("corgi-test-fake-container-xyz")
	if err != nil {
		t.Skip("docker not available:", err)
	}
	if running {
		t.Error("expected not running")
	}
}

// installFakeDockerPS writes a `docker ps` stub that mimics how the real
// daemon treats the `--filter name=...` value: an anchored `^name$` matches
// only the exact container, while a bare `name` substring-matches every
// container whose name contains it. running is the set of up containers; the
// stub prints "<name>\tUp 3 seconds" for each that satisfies the filter.
func installFakeDockerPS(t *testing.T, running []string) {
	t.Helper()
	binDir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString(`filter=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--filter" ]; then filter=$(echo "$arg" | sed 's/^name=//'); fi
  prev="$arg"
done
anchored=0
case "$filter" in
  ^*$) anchored=1; filter=$(echo "$filter" | sed -e 's/^\^//' -e 's/\$$//') ;;
esac
emit() {
  if [ "$anchored" = "1" ]; then
    [ "$1" = "$filter" ] && printf '%s\tUp 3 seconds\n' "$1"
  else
    case "$1" in *"$filter"*) printf '%s\tUp 3 seconds\n' "$1" ;; esac
  fi
}
`)
	for _, name := range running {
		fmt.Fprintf(&sb, "emit %s\n", name)
	}
	sb.WriteString("exit 0\n")

	script := filepath.Join(binDir, "docker")
	if err := os.WriteFile(script, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestIsServiceRunningExactMatch(t *testing.T) {
	// "postgres-main-replica" is up but "postgres-main" is NOT — an unanchored
	// substring filter would wrongly report postgres-main as running.
	installFakeDockerPS(t, []string{"postgres-main-replica"})

	if ok, err := IsServiceRunning("postgres-main"); err != nil || ok {
		t.Fatalf("postgres-main must be down (only the replica is up): ok=%v err=%v", ok, err)
	}
	if ok, err := IsServiceRunning("postgres-main-replica"); err != nil || !ok {
		t.Fatalf("postgres-main-replica should be running: ok=%v err=%v", ok, err)
	}
	// A wholly absent container is down.
	if ok, err := IsServiceRunning("absent"); err != nil || ok {
		t.Fatalf("absent must be down: ok=%v err=%v", ok, err)
	}
}

func TestGetStatusOfServiceFakeContainer(t *testing.T) {
	running, err := GetStatusOfService("corgi-test-fake-container-xyz")
	if err != nil {
		t.Skip("docker not available:", err)
	}
	if running {
		t.Error("expected not running")
	}
}

func TestGetLocalMachineIpAddress(t *testing.T) {
	ip, _ := GetLocalMachineIpAddress()
	_ = ip
}

func TestGetContainerIdNoMakefile(t *testing.T) {
	_, err := GetContainerId("corgi-nonexistent-svc-xyz")
	if err == nil {
		t.Error("expected error for nonexistent service")
	}
}

func TestStartDockerAndWait_BailsBeforeLaunchingDockerApp(t *testing.T) {
	// If docker is already running, the function would return nil before
	// hitting StartDocker — that path doesn't prove anything about the
	// shutdown short-circuit, so skip.
	if CheckDockerStatus() == nil {
		t.Skip("docker already running")
	}
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)

	// Pre-signal shutdown. startDockerAndWait must return BEFORE invoking
	// StartDocker (which would `open /Applications/Docker.app` on macOS).
	RequestShutdown()

	start := time.Now()
	err := startDockerAndWait()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected abort error, got nil")
	}
	if !strings.Contains(err.Error(), "aborted by shutdown signal") {
		t.Errorf("expected abort-by-shutdown error, got: %v", err)
	}
	// Must return effectively instantly — never touch StartDocker or the
	// 60s poll deadline.
	if elapsed > 500*time.Millisecond {
		t.Errorf("did not short-circuit promptly: %s — may have launched Docker", elapsed)
	}
}
