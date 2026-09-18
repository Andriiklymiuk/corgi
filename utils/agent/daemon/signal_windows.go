//go:build windows

package daemon

import "os"

var syscallZero os.Signal = nil

func processAliveOS(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}

func nudgeProcess(int) error { return nil }

func notifyNudge(chan<- struct{}) func() {
	return func() {
	}
}
