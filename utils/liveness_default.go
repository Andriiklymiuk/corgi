//go:build !windows

package utils

import "syscall"

// PidAlive reports whether pid is a live process we can signal. command is
// reserved for future cmdline verification against pid reuse.
func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
