package utils

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

var consoleOverride atomic.Pointer[io.Writer]

func SetConsoleOverride(w io.Writer) { consoleOverride.Store(&w) }

func ClearConsoleOverride() { consoleOverride.Store(nil) }

func OverrideWriter() io.Writer {
	if p := consoleOverride.Load(); p != nil {
		return *p
	}
	return nil
}

var consoleMirror atomic.Pointer[io.Writer]

func SetConsoleMirror(w io.Writer) { consoleMirror.Store(&w) }

func ClearConsoleMirror() { consoleMirror.Store(nil) }

func MirrorWriter() io.Writer {
	if p := consoleMirror.Load(); p != nil {
		return *p
	}
	return nil
}

func withMirror(base io.Writer) io.Writer {
	if m := MirrorWriter(); m != nil {
		return io.MultiWriter(base, m)
	}
	return base
}

func WithMirror(base io.Writer) io.Writer { return withMirror(base) }

var PayloadOnStdout bool

func infoWriter() io.Writer {
	if w := OverrideWriter(); w != nil {
		return withMirror(w)
	}
	if JSONOutput || PayloadOnStdout {
		return withMirror(os.Stderr)
	}
	return withMirror(os.Stdout)
}

func Info(a ...any) {
	fmt.Fprintln(infoWriter(), a...)
}

func Infof(format string, a ...any) {
	fmt.Fprintf(infoWriter(), format, a...)
}

func ConsoleOut() io.Writer {
	if w := OverrideWriter(); w != nil {
		return withMirror(w)
	}
	if JSONOutput {
		return withMirror(os.Stderr)
	}
	return withMirror(os.Stdout)
}
