package utils

import (
	"testing"
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
