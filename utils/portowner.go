package utils

import (
	"strconv"
	"strings"
)

type portListener struct {
	PID  int
	Name string
}

// parseProcNetTCP returns the socket inodes listening on port from one
// /proc/net/tcp or /proc/net/tcp6 table.
func parseProcNetTCP(table string, port int) []uint64 {
	const listen = "0A"
	var inodes []uint64
	for i, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) < 10 || fields[3] != listen {
			continue
		}
		_, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		p, err := strconv.ParseUint(portHex, 16, 32)
		if err != nil || int(p) != port {
			continue
		}
		if inode, err := strconv.ParseUint(fields[9], 10, 64); err == nil && inode != 0 {
			inodes = append(inodes, inode)
		}
	}
	return inodes
}
