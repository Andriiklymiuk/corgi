package utils

import (
	"fmt"
	"io"
	"os"
)

// infoWriter is where human-facing log lines go: stderr in JSON mode so the
// JSON payload on stdout stays clean, stdout otherwise.
func infoWriter() io.Writer {
	if JSONOutput {
		return os.Stderr
	}
	return os.Stdout
}

// Info prints a human-facing informational line (Println semantics).
func Info(a ...any) {
	fmt.Fprintln(infoWriter(), a...)
}

// Infof prints a formatted human-facing informational line.
func Infof(format string, a ...any) {
	fmt.Fprintf(infoWriter(), format, a...)
}
