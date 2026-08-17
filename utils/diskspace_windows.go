//go:build windows

package utils

// FreeDiskBytes is unimplemented here, so the check reports unknown.
func FreeDiskBytes(_ string) (uint64, bool) { return 0, false }
