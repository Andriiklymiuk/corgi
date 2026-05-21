package utils

import (
	"os/exec"
	"strings"
)

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
