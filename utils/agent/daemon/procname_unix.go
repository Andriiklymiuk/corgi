//go:build !windows

package daemon

import (
	"os/exec"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils/agent/proc"
)

const kernelNameLimit = 15

func processName(pid int) (string, bool) {
	if p, ok := proc.Lookup(pid); ok && p.Name != "" && len(p.Name) <= kernelNameLimit {
		return filepath.Base(p.Name), true
	}
	out, err := exec.Command("ps", "-p", itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", false
	}
	return filepath.Base(name), true
}
