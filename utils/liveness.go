package utils

import (
	"os/exec"
	"strings"
)

// ContainerRunning probes a container by exact name via docker inspect,
// returning "running", "stopped", or "" when the state is unknown (no such
// container, docker unavailable, etc.).
func ContainerRunning(name string) string {
	if name == "" {
		return ""
	}
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(string(out)) {
	case "true":
		return "running"
	case "false":
		return "stopped"
	default:
		return ""
	}
}
