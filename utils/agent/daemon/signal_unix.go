//go:build !windows

package daemon

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

var syscallZero = syscall.Signal(0)

func processAliveOS(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscallZero)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func nudgeProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGUSR1)
}

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
