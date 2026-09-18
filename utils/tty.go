package utils

import "os"

func fileIsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func IsTTY() bool {
	return fileIsTTY(os.Stdout)
}

func StdinIsTTY() bool {
	return fileIsTTY(os.Stdin)
}
