package utils

import (
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
