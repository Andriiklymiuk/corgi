//go:build windows

package daemon

func processName(int) (string, bool) { return "", false }
