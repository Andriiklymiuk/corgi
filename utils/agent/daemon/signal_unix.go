//go:build !windows

package daemon

import (
	"os"
	"os/signal"
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

// nudgeProcess delivers the spool doorbell to a daemon in another process.
func nudgeProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGUSR1)
}

// notifyNudge forwards SIGUSR1 into the command loop's wake channel until the
// returned stop func runs. The channel send never blocks: a burst of signals
// coalesces into one drain, which reads the whole spool anyway.
func notifyNudge(ch chan<- struct{}) func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				select {
				case ch <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
	}
}
