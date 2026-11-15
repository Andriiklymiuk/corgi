//go:build windows

package term

import (
	"errors"
	"io"
	"os/exec"
)

func startPTY(*exec.Cmd, uint16, uint16) (io.ReadWriteCloser, *exec.Cmd, error) {
	return nil, nil, errors.New("terminals are for macOS and Linux machines")
}

func resizePTY(io.ReadWriteCloser, uint16, uint16) {
	// No terminals on Windows: startPTY refuses first.
}
