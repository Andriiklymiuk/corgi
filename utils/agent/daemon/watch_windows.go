//go:build windows

package daemon

import "os/exec"

func killProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Kill() }
}
