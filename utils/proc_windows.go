//go:build windows
// +build windows

package utils

import (
	"os"
	"os/exec"
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
