//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// syscallZero is signal 0: it performs the permission and existence checks
// without delivering anything, which is the portable liveness probe.
var syscallZero = syscall.Signal(0)

// processAliveOS reports whether pid is a live process. On unix os.FindProcess
// always succeeds, so the signal probe is what actually answers.
func processAliveOS(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscallZero) == nil
}
