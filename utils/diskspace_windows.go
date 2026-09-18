//go:build windows

package utils

func FreeDiskBytes(_ string) (uint64, bool) { return 0, false }
