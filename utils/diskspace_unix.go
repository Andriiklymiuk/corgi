//go:build !windows

package utils

import "syscall"

func FreeDiskBytes(path string) (free uint64, ok bool) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, false
	}
	return uint64(fs.Bavail) * uint64(fs.Bsize), true
}
