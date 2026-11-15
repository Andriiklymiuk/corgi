//go:build !windows

package cmd

import (
	"os"
	"syscall"

	xterm "golang.org/x/term"
)

func ttySizeOf(tty string) (int, int, bool) {
	f, err := os.OpenFile(tty, os.O_RDONLY|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	w, h, err := xterm.GetSize(int(f.Fd()))
	return w, h, err == nil && w > 0 && h > 0
}
