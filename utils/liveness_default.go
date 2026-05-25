//go:build !windows

package utils

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// PidAlive reports whether pid is live AND still running command. The cmdline
// check guards against PID reuse: signaling a recycled pgid would hit an
// unrelated process group. Unreadable cmdline → assume alive (safe); readable
// mismatch → dead.
func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	if command == "" {
		return true
	}
	actual, ok := processCommandLine(pid)
	if !ok {
		return true
	}
	return strings.Contains(actual, commandNeedle(command))
}

// processCommandLine returns pid's command line via ps (works on darwin + linux).
func processCommandLine(pid int) (string, bool) {
	out, err := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// commandNeedle takes the first &&-segment (capped) as a stable match token.
func commandNeedle(command string) string {
	needle := command
	if i := strings.Index(needle, " && "); i > 0 {
		needle = needle[:i]
	}
	needle = strings.TrimSpace(needle)
	if len(needle) > 60 {
		needle = needle[:60]
	}
	return needle
}
