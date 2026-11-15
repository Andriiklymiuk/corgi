//go:build windows

package screen

import "os"

func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) {}
