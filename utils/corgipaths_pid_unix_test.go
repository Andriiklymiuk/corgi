//go:build !windows

package utils

import (
	"os/exec"
	"syscall"
	"testing"
)

// livePID is a process corgi would recognise as its own: alive, and its own
// group leader, the way a detached service is started.
func livePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}
