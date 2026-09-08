//go:build darwin

package proc

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// lookup asks the kernel directly: one sysctl, microseconds, no fork. The name
// it returns is the 16-character p_comm, which is all a hook needs to tell a
// shell from claude.
func lookup(pid int) (Process, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return Process{}, false
	}
	return Process{PID: pid, PPID: int(kp.Eproc.Ppid), Name: comm(kp.Proc.P_comm[:]), TTY: ttyDev(kp.Eproc.Tdev)}, true
}

// ttyDev is e_tdev as a device number, with the kernel's NODEV (-1) mapped
// to 0.
func ttyDev(tdev int32) uint64 {
	if tdev < 0 {
		return 0
	}
	return uint64(tdev)
}

// TTYName resolves a device number to its /dev path by matching st_rdev
// over the pty entries. A few hundred stats at most, and only at focus time.
func TTYName(dev uint64) string {
	if dev == 0 {
		return ""
	}
	return findDev("/dev", "ttys", dev)
}

func comm(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
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
	return proc.Signal(unix.Signal(0)) == nil
}

// List enumerates every process the caller may see, with the full command
// line where ps will give it. The names come from the kernel; ps is asked once
// for the arguments because that is the only way to tell a node process
// running Claude Code from any other.
func List() ([]Process, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	args := argsByPID()
	out := make([]Process, 0, len(kps))
	for _, kp := range kps {
		pid := int(kp.Proc.P_pid)
		if pid <= 0 {
			continue
		}
		out = append(out, Process{PID: pid, PPID: int(kp.Eproc.Ppid), Name: comm(kp.Proc.P_comm[:]), Args: args[pid], TTY: ttyDev(kp.Eproc.Tdev)})
	}
	return out, nil
}

func argsByPID() map[int]string {
	out := map[int]string{}
	raw, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		pidStr, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		out[pid] = strings.TrimSpace(rest)
	}
	return out
}

// Cwd is the process's working directory, via lsof: there is no cgo-free
// proc_pidinfo. Only rescan asks, once per unknown session, so a fork is fine.
func Cwd(pid int) string {
	raw, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "n") {
			return strings.TrimSpace(line[1:])
		}
	}
	return ""
}
