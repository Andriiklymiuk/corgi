//go:build windows

package daemon

// processName cannot be determined cheaply on Windows without extra syscalls,
// and agent mode does not install there, so the check is skipped.
func processName(int) (string, bool) { return "", false }
