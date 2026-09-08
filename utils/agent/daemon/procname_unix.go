//go:build !windows

package daemon

import (
	"os/exec"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils/agent/proc"
)

// kernelNameLimit is the longest name the kernel's own table holds (macOS
// MAXCOMLEN, Linux TASK_COMM_LEN minus the terminator). A name that long may
// be cut short, and ps is asked for the full one.
const kernelNameLimit = 15

// processName returns the executable's base name for pid, and whether it could
// be determined. The kernel is asked first — a syscall or a /proc read, which
// matters because a hook asks on every tool call — and ps only when the
// kernel's short name might be truncated.
//
// macOS prints the full path for `-o comm=` while Linux prints just the name,
// so the result is reduced to a base name either way.
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
