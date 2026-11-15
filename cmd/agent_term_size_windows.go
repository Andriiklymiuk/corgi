//go:build windows

package cmd

func watchTerminalSize(int, chan<- [2]uint16) func() {
	return func() {
		// Windows sends no resize signal; the first size holds.
	}
}
