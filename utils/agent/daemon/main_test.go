package daemon

import (
	"os"
	"testing"
)

// Tests run on whatever laptop runs them: the machine, the lid and the
// network are not read for real.
func TestMain(m *testing.M) {
	readBattery = func() (int, bool, float64, bool) { return -1, true, 0, false }
	readThermal = func() (int, bool) { return 0, false }
	readClamshell = func() (bool, bool) { return false, false }
	readOnline = func() bool { return true }
	readWifiInterface = func() string { return "" }
	hasExternalDisplay = func() bool { return false }
	readFreeGB = func(string) (int, bool) { return 200, true }
	os.Exit(m.Run())
}
