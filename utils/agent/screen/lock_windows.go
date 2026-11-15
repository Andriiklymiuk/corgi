//go:build windows

package screen

import "os"

func lockFile(*os.File) error {
	// Windows never hosts the screen; the in-process mutex is enough there.
	return nil
}

func unlockFile(*os.File) {
	// Nothing was locked: see lockFile.
}
