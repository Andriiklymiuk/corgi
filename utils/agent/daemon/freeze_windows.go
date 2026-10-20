//go:build windows

package daemon

import "errors"

var stopProcess = func(int) error { return errors.New("pausing a process is not supported on windows") }

var continueProcess = func(int) error { return nil }
