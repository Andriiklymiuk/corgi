//go:build !windows

package utils

import "syscall"

// FreeDiskBytes reports space available at path; ok is false when unknown.
func FreeDiskBytes(path string) (free uint64, ok bool) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, false
	}
	// Bavail, not Bfree: reserved blocks are not ours.
	return uint64(fs.Bavail) * uint64(fs.Bsize), true
}
