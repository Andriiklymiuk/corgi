//go:build !windows

package utils

import "syscall"

func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	pgid, ok := processPGID(pid)
	if !ok {
		return true
	}
	return pgid == pid
}

func processPGID(pid int) (int, bool) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, false
	}
	return pgid, true
}
