//go:build !darwin && !linux

package proc

import "errors"

// On a platform without a cheap process table corgi tracks nothing: a hook
// that cannot say which process it belongs to has nothing to report.

func lookup(int) (Process, bool) { return Process{}, false }

// Alive cannot be answered without a probe; report false so a reaper on this
// platform never keeps a session it cannot verify.
func Alive(int) bool { return false }

// List is unsupported here.
func List() ([]Process, error) {
	return nil, errors.New("process listing is not supported on this platform")
}

// Cwd is unknown here.
func Cwd(int) string { return "" }
