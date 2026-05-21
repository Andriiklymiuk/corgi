//go:build !windows

package utils

import "syscall"

func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
