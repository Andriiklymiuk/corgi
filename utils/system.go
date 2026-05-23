package utils

import (
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
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return err
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	if len(pids) == 0 {
		return nil
	}
	return exec.Command("kill", pids...).Run()
}
