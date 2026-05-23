package utils

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// StartDockerDaemon best-effort starts the local Docker engine.
func StartDockerDaemon() error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-a", "Docker").Run()
	case "linux":
		return exec.Command("systemctl", "start", "docker").Run()
	default:
		return exec.Command("cmd", "/c", "start", "", "Docker Desktop").Start()
	}
}

// KillPortOwner terminates the process listening on port (best effort).
func KillPortOwner(port int) error {
	p := strconv.Itoa(port)
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c",
			"for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :"+p+"') do taskkill /F /PID %a").Run()
	}
	out, err := exec.Command("lsof", "-t", "-i:"+p).Output()
	if err != nil {
		return err
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return nil
	}
	// lsof can return several pids, one per line; kill them all.
	return exec.Command("kill", strings.Fields(pid)...).Run()
}
