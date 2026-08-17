//go:build !windows

package utils

import "syscall"

// FreeDiskBytes reports the space available at path. ok is false when the
// platform cannot answer, so a caller can skip the check rather than guess.
func FreeDiskBytes(path string) (free uint64, ok bool) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, false
	}
	// Bavail, not Bfree: the reserved blocks are not ours to fill.
	return uint64(fs.Bavail) * uint64(fs.Bsize), true
}
