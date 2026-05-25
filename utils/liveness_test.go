package utils

import (
	"os"
	"testing"
)

func TestPidAliveSelf(t *testing.T) {
	// PidAlive also requires group-leadership (PID-reuse guard); whether the
	// test process leads its own group depends on how it was launched, so only
	// assert the call is side-effect-free here. Live+leader is covered by
	// TestPidAliveGroupLeader.
	_ = PidAlive(os.Getpid(), "")
}

func TestPidAliveNonPositive(t *testing.T) {
	if PidAlive(0, "") {
		t.Error("pid 0 should not report alive")
	}
	if PidAlive(-1, "") {
		t.Error("negative pid should not report alive")
	}
}
