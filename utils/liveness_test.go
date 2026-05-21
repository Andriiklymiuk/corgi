package utils

import (
	"os"
	"testing"
)

func TestPidAliveSelf(t *testing.T) {
	if !PidAlive(os.Getpid(), "") {
		t.Error("current process should be alive")
	}
}

func TestPidAliveNonPositive(t *testing.T) {
	if PidAlive(0, "") {
		t.Error("pid 0 should not report alive")
	}
	if PidAlive(-1, "") {
		t.Error("negative pid should not report alive")
	}
}
