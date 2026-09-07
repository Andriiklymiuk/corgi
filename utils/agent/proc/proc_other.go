//go:build !darwin && !linux

package proc

import (
	"errors"
	"os"
)

// On a platform without a cheap process table corgi tracks nothing: a hook
// that cannot say which process it belongs to has nothing to report.

func lookup(int) (Process, bool) { return Process{}, false }

// Alive asks the OS for a handle: on Windows os.FindProcess fails for a pid
// that is gone, which is the one probe available without a process table.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

// List is unsupported here.
func List() ([]Process, error) {
	return nil, errors.New("process listing is not supported on this platform")
}

// Cwd is unknown here.
func Cwd(int) string { return "" }
