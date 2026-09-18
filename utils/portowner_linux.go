//go:build linux

package utils

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nativeListeners finds who listens on port from /proc alone, so a box
// without lsof still gets a name. ok is false when /proc gave nothing.
func nativeListeners(port int) (owners []portListener, ok bool) {
	want := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, inode := range parseProcNetTCP(string(data), port) {
			want["socket:["+strconv.FormatUint(inode, 10)+"]"] = true
		}
	}
	if len(want) == 0 {
		return nil, false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !want[target] {
				continue
			}
			owners = append(owners, portListener{PID: pid, Name: procComm(pid)})
			break
		}
	}
	return owners, len(owners) > 0
}

func procComm(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(data))
}
