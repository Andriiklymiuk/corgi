//go:build windows
// +build windows

package utils

import (
	"os"
	"os/exec"
	"syscall"
)

// SetProcessGroup is a placeholder for Windows, where process groups are handled differently.
func SetProcessGroup(cmd *exec.Cmd) {
	// Windows does not support Setpgid in the same way Unix does.
	// You might need to use Job Objects if you need similar functionality.
}

func KillProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// SignalProcessGroup is best-effort on Windows: there is no process-group
// signalling, so it kills the single process regardless of sig.
func SignalProcessGroup(pgid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pgid)
	if err != nil {
		return err
	}
	return process.Kill()
}
