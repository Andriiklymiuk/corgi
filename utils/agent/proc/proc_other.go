//go:build !darwin && !linux

package proc

import (
	"errors"
	"os"
)

func lookup(int) (Process, bool) { return Process{}, false }

func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

func List() ([]Process, error) {
	return nil, errors.New("process listing is not supported on this platform")
}

func Cwd(int) string { return "" }

func TTYName(uint64) string { return "" }
