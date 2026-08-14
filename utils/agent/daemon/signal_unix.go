//go:build !windows

package daemon

import "syscall"

// syscallZero is signal 0: it performs the permission and existence checks
// without delivering anything, which is the portable liveness probe.
var syscallZero = syscall.Signal(0)
