package utils

import "os"

// fileIsTTY reports whether f is a character device (an interactive terminal).
func fileIsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// IsTTY reports whether stdout is attached to an interactive terminal.
func IsTTY() bool {
	return fileIsTTY(os.Stdout)
}

// StdinIsTTY reports whether stdin is attached to an interactive terminal.
func StdinIsTTY() bool {
	return fileIsTTY(os.Stdin)
}
