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
