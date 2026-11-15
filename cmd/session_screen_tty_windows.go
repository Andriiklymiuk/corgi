//go:build windows

package cmd

func ttySizeOf(string) (int, int, bool) { return 0, 0, false }
