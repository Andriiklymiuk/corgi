package utils

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// dockerStartCommand returns the OS-specific command to start the local Docker
// engine. Pure: builds the argv, runs nothing — so it stays unit-testable.
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

// StartDockerDaemon best-effort starts the local Docker engine.
func StartDockerDaemon() error {
	name, args := dockerStartCommand(runtime.GOOS)
	return exec.Command(name, args...).Run()
}

// killPortCommand returns the OS-specific command that frees a TCP port. The
// port is an int (no shell-injection surface) and every token is a fixed
// literal except that number. Pure: builds the argv, runs nothing.
func killPortCommand(goos string, port int) (string, []string) {
	p := strconv.Itoa(port)
	if goos == "windows" {
		return "cmd", []string{"/c",
			"for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :" + p + "') do taskkill /F /PID %a"}
	}
	// unix: resolve the pid(s) via lsof, then kill — handled by KillPortOwner.
	return "lsof", []string{"-t", "-i:" + p}
}

// KillPortOwner terminates the process listening on port (best effort).
func KillPortOwner(port int) error {
	name, args := killPortCommand(runtime.GOOS, port)
	if runtime.GOOS == "windows" {
		return exec.Command(name, args...).Run()
	}
	lsof := lsofPath()
	if lsof == "" {
		return fmt.Errorf(
			"lsof not found on PATH or in /usr/sbin, /usr/bin, /sbin — cannot identify the process on port %d",
			port)
	}
	// lsof exits non-zero when nothing matches — decide on the parsed pids.
	out, _ := exec.Command(lsof, args...).Output()
	pids := strings.Fields(strings.TrimSpace(string(out)))
	if len(pids) == 0 {
		return fmt.Errorf(
			"no process found listening on port %d (it may be owned by another user — try: sudo lsof -nP -i:%d)",
			port, port)
	}
	if err := exec.Command("kill", pids...).Run(); err != nil {
		return fmt.Errorf("failed to kill pid(s) %s on port %d: %w",
			strings.Join(pids, ","), port, err)
	}
	return nil
}
