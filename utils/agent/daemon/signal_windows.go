//go:build windows

package daemon

import "os"

// Windows has no signal 0. os.FindProcess already fails for a dead pid there,
// so the probe is a no-op signal that always reports reachable.
var syscallZero os.Signal = nil
