//go:build !windows

package utils

import (
	"os/exec"
	"syscall"
)

func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func KillProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
