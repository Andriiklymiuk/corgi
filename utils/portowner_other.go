//go:build !linux

package utils

func nativeListeners(int) ([]portListener, bool) { return nil, false }
