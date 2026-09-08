//go:build linux

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// lookup reads /proc/<pid>/stat: "pid (comm) state ppid ...". The comm may
// contain spaces and parentheses, so the split is on the LAST ')'.
func lookup(pid int) (Process, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return Process{}, false
	}
	return parseStat(pid, string(data))
}

func parseStat(pid int, stat string) (Process, bool) {
	open := strings.IndexByte(stat, '(')
	close := strings.LastIndexByte(stat, ')')
	if open < 0 || close < open {
		return Process{}, false
	}
	fields := strings.Fields(stat[close+1:])
	if len(fields) < 2 {
		return Process{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return Process{}, false
	}
	p := Process{PID: pid, PPID: ppid, Name: stat[open+1 : close]}
	if len(fields) > 4 {
		// tty_nr: 0 when there is no controlling terminal.
		if tty, err := strconv.ParseUint(fields[4], 10, 64); err == nil {
			p.TTY = tty
		}
	}
	if cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")); err == nil {
		p.Args = strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
	}
	return p, true
}

// Alive is the classic signal-0 probe.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// List walks /proc.
func List() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		if p, ok := lookup(pid); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// Cwd resolves the /proc cwd link.
func Cwd(pid int) string {
	dir, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return dir
}

// TTYName resolves a device number to its /dev/pts path.
func TTYName(dev uint64) string {
	if dev == 0 {
		return ""
	}
	return findDev("/dev/pts", "", dev)
}
