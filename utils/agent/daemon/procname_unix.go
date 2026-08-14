//go:build !windows

package daemon

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// processName returns the executable's base name for pid, and whether it could
// be determined. Uses ps, which is present on macOS and Linux.
//
// macOS prints the full path for `-o comm=` while Linux prints just the name,
// so the result is reduced to a base name either way.
func processName(pid int) (string, bool) {
	out, err := exec.Command("ps", "-p", itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", false
	}
	return filepath.Base(name), true
}
