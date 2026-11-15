//go:build !windows

package term

import (
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, cols, rows uint16) (io.ReadWriteCloser, *exec.Cmd, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: max(cols, 20), Rows: max(rows, 5)})
	if err != nil {
		return nil, nil, err
	}
	return f, cmd, nil
}

func resizePTY(tty io.ReadWriteCloser, cols, rows uint16) {
	if f, ok := tty.(*os.File); ok {
		_ = pty.Setsize(f, &pty.Winsize{Cols: max(cols, 20), Rows: max(rows, 5)})
	}
}
