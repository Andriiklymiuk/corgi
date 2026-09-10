//go:build windows

package utils

import "testing"

func livePID(t *testing.T) int {
	t.Helper()
	t.Skip("no process-group leader to stand in for a detached service on windows")
	return 0
}
