//go:build !windows

package utils

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestPidAliveGroupLeader(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid

	if !PidAlive(pid, "") {
		t.Error("group-leader process should report alive")
	}
	if !PidAlive(pid, "sleep 30") {
		t.Error("command arg must not change the result")
	}
}

func TestPidAliveNonLeader(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if PidAlive(cmd.Process.Pid, "") {
		t.Error("non-group-leader pid must report dead (PID-reuse guard)")
	}
}
