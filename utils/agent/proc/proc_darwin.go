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

func lookup(pid int) (Process, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return Process{}, false
	}
	return Process{PID: pid, PPID: int(kp.Eproc.Ppid), Name: comm(kp.Proc.P_comm[:]), TTY: ttyDev(kp.Eproc.Tdev)}, true
}

func ttyDev(tdev int32) uint64 {
	if tdev < 0 {
		return 0
	}
	return uint64(tdev)
}

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
