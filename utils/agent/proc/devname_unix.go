//go:build darwin || linux

package proc

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// findDev returns the entry under dir (with the given name prefix) whose
// character-device number is dev, or "".
func findDev(dir, prefix string, dev uint64) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if prefix != "" && !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(path)
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && uint64(st.Rdev) == dev {
			return path
		}
	}
	return ""
}
