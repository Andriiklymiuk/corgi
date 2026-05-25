//go:build !windows

package utils

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// PidAlive reports whether pid is a live process still owned by corgi. The bare
// kill(pid,0) check isn't enough: after a detached service exits the OS recycles
// its PID, and signaling that recycled pgid would hit an unrelated process group.
//
// Detached procs are process-group leaders (Setpgid → pgid==pid), so we confirm
// pid is still its own group leader. That survives the child exec'ing into
// another binary (npm→node) yet rejects a recycled pid, which is almost never a
// group leader. Unreadable pgid → assume alive (safe: at worst we orphan).
// command is unused here; kept for signature/back-compat.
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

// processPGID returns pid's process-group id via ps (works on darwin + linux).
func processPGID(pid int) (int, bool) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pgid=").Output()
	if err != nil {
		return 0, false
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return pgid, true
}
