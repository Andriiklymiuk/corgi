//go:build !windows

package daemon

import "syscall"

var stopProcess = func(pid int) error { return syscall.Kill(pid, syscall.SIGSTOP) }

var continueProcess = func(pid int) error { return syscall.Kill(pid, syscall.SIGCONT) }
