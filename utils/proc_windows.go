//go:build windows

package utils

import (
	"os"
	"os/exec"
	"syscall"
)

func SetProcessGroup(cmd *exec.Cmd) {
}

func KillProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func SignalProcessGroup(pgid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pgid)
	if err != nil {
		return err
	}
	return process.Kill()
}
