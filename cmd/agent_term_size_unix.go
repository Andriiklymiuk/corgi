//go:build !windows

package cmd

import (
	"os"
	"os/signal"
	"syscall"

	xterm "golang.org/x/term"
)

func watchTerminalSize(fd int, sizes chan<- [2]uint16) func() {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			if w, h, err := xterm.GetSize(fd); err == nil {
				select {
				case sizes <- [2]uint16{uint16(w), uint16(h)}:
				default:
				}
			}
		}
	}()
	return func() {
		signal.Stop(winch)
		close(winch)
	}
}
