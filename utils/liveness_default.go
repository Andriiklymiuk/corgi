//go:build !windows

package utils

import "syscall"

// PidAlive reports whether pid is a live process still owned by corgi.
//
// kill(pid,0) alone is not enough: a recycled PID would make corgi signal an
// unrelated process group. Detached procs are group leaders (Setpgid), so pid
// must still be its own — that survives npm exec'ing into node but rejects a
// recycled pid. Unreadable pgid assumes alive; at worst corgi orphans it.
// command is unused, kept for back-compat.
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

// processPGID returns pid's process-group id via getpgid(2) — no fork.
func processPGID(pid int) (int, bool) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, false
	}
	return pgid, true
}
