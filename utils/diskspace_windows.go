//go:build windows

package utils

// FreeDiskBytes has no portable implementation here yet, so the headroom check
// reports "unknown" rather than a wrong number.
func FreeDiskBytes(_ string) (uint64, bool) { return 0, false }
