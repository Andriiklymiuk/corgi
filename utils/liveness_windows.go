//go:build windows

package utils

import "os"

// PidAlive is best-effort on Windows: FindProcess succeeds for any pid, so this
// is a conservative existence check. Detached lifecycle is limited on Windows.
func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
