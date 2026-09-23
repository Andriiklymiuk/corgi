package utils

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func dockerStartCommand(goos string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"-a", "Docker"}
	case "linux":
		return "systemctl", []string{"start", "docker"}
	default:
		return "cmd", []string{"/c", "start", "", "Docker Desktop"}
	}
}

func StartDockerDaemon() error {
	name, args := dockerStartCommand(runtime.GOOS)
	return exec.Command(name, args...).Run()
}

func killPortCommand(goos string, port int) (string, []string) {
	p := strconv.Itoa(port)
	if goos == "windows" {
		return "cmd", []string{"/c",
			"for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :" + p + "') do taskkill /F /PID %a"}
	}
	return "lsof", []string{"-t", "-i:" + p}
}

// ListenerPIDs names the processes listening on port: /proc on Linux, lsof
// elsewhere (and on a Linux box where /proc showed nothing).
func ListenerPIDs(port int) []int {
	if owners, ok := nativeListeners(port); ok {
		pids := make([]int, 0, len(owners))
		for _, o := range owners {
			pids = append(pids, o.PID)
		}
		return pids
	}
	lsof := lsofPath()
	if lsof == "" {
		return nil
	}
	out, _ := exec.Command(lsof, "-t", "-i:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(field); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func KillPortOwner(port int) error {
	name, args := killPortCommand(runtime.GOOS, port)
	if runtime.GOOS == "windows" {
		return exec.Command(name, args...).Run()
	}
	var pids []string
	if owners, ok := nativeListeners(port); ok {
		for _, o := range owners {
			pids = append(pids, strconv.Itoa(o.PID))
		}
	} else {
		lsof := lsofPath()
		if lsof == "" {
			return fmt.Errorf(
				"lsof not found on PATH or in /usr/sbin, /usr/bin, /sbin - cannot identify the process on port %d",
				port)
		}
		out, _ := exec.Command(lsof, args...).Output()
		pids = strings.Fields(strings.TrimSpace(string(out)))
	}
	if len(pids) == 0 {
		return fmt.Errorf(
			"no process found listening on port %d (it may be owned by another user - try: sudo lsof -nP -i:%d)",
			port, port)
	}
	if err := exec.Command("kill", pids...).Run(); err != nil {
		return fmt.Errorf("failed to kill pid(s) %s on port %d: %w",
			strings.Join(pids, ","), port, err)
	}
	return nil
}
