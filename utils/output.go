package utils

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// consoleOverride, when non-nil, redirects human-facing log output and live
// process console output to an explicit writer. Used by the MCP server to
// keep the JSON-RPC stdout channel clean WITHOUT mutating the process-global
// os.Stdout (which would race under the concurrent HTTP transport).
var consoleOverride atomic.Pointer[io.Writer]

// SetConsoleOverride redirects Info/Infof/ConsoleOut to w. Goroutine-safe.
func SetConsoleOverride(w io.Writer) { consoleOverride.Store(&w) }

// ClearConsoleOverride restores default stdout/stderr routing.
func ClearConsoleOverride() { consoleOverride.Store(nil) }

// OverrideWriter returns the active override (nil when unset). For tests.
func OverrideWriter() io.Writer {
	if p := consoleOverride.Load(); p != nil {
		return *p
	}
	return nil
}

// infoWriter is where human-facing log lines go: the console override when set,
// else stderr in JSON mode so the JSON payload on stdout stays clean, else
// stdout.
func infoWriter() io.Writer {
	if w := OverrideWriter(); w != nil {
		return w
	}
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

// ConsoleOut is the stream for streamed/live process output: the console
// override when set, else stderr in JSON mode (so stdout stays the pure JSON
// payload), else stdout.
func ConsoleOut() io.Writer {
	if w := OverrideWriter(); w != nil {
		return w
	}
	if JSONOutput {
		return os.Stderr
	}
	return os.Stdout
}
