//go:build windows

package utils

import "os"

func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
