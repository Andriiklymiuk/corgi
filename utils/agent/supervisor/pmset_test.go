package supervisor

import (
	"os"
	"strings"
	"testing"
)

func fakePmset(t *testing.T, disabled bool) *[]string {
	t.Helper()
	oldRun, oldG := runSudo, readPmsetG
	t.Cleanup(func() { runSudo, readPmsetG = oldRun, oldG })
	var calls []string
	runSudo = func(args ...string) error { calls = append(calls, strings.Join(args, " ")); return nil }
	readPmsetG = func() string {
		if disabled {
			return "System-wide power settings:\n SleepDisabled\t\t1\n"
		}
		return "System-wide power settings:\n SleepDisabled\t\t0\n"
	}
	return &calls
}

func TestDisableSleepUndoesOnlyWhatItSet(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	calls := fakePmset(t, true)
	if err := DisableSleep(dir, true); err != nil || len(*calls) != 0 {
		t.Fatalf("already off by hand: nothing to do, got %v %v", err, *calls)
	}
	if err := DisableSleep(dir, false); err != nil || len(*calls) != 0 {
		t.Fatalf("not ours to undo: %v %v", err, *calls)
	}
	calls = fakePmset(t, false)
	if err := DisableSleep(dir, true); err != nil || !strings.Contains(strings.Join(*calls, ";"), "-a disablesleep 1") {
		t.Fatalf("set it: %v %v", err, *calls)
	}
	if !readPower(dir).DisableSleep {
		t.Fatal("written down")
	}
	if err := DisableSleep(dir, false); err != nil || !strings.Contains(strings.Join(*calls, ";"), "-a disablesleep 0") {
		t.Fatalf("undone: %v %v", err, *calls)
	}
	if _, err := os.Stat(powerPath(dir)); !os.IsNotExist(err) {
		t.Fatal("the journal is empty again")
	}
}

func TestLowPowerModeIsSetOnce(t *testing.T) {
	dir := t.TempDir()
	calls := fakePmset(t, true)
	_ = LowPowerMode(dir, true)
	_ = LowPowerMode(dir, true)
	_ = LowPowerMode(dir, false)
	_ = LowPowerMode(dir, false)
	if got := strings.Join(*calls, ";"); got != "-b lowpowermode 1;-b lowpowermode 0" {
		t.Fatalf("got %q", got)
	}
}

func TestSudoersRuleNamesTheFourLines(t *testing.T) {
	rule := SudoersRule()
	for _, want := range []string{"NOPASSWD", "disablesleep 1", "disablesleep 0", "lowpowermode 1", "lowpowermode 0"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("%q lacks %q", rule, want)
		}
	}
	if !strings.Contains(SudoersInstall(), "/etc/sudoers.d/corgi") {
		t.Fatal(SudoersInstall())
	}
}

func TestSleepNowClearsSleepDisabledAndRearms(t *testing.T) {
	dir := t.TempDir()
	calls := fakePmset(t, true)
	oldCheck := checkSudo
	t.Cleanup(func() { checkSudo = oldCheck; ResetPmsetAllowed() })
	checkSudo = func() bool { return true }
	ResetPmsetAllowed()
	_ = SleepNow(dir)
	if got := strings.Join(*calls, ";"); got != "-a disablesleep 0" {
		t.Fatalf("the bit is cleared first: %q", got)
	}
	if !readPower(dir).RearmSleep {
		t.Fatal("written down for the wake")
	}
	if err := RearmAfterSleep(dir); err != nil || !strings.HasSuffix(strings.Join(*calls, ";"), "-a disablesleep 1") {
		t.Fatalf("put back at the wake: %v %v", err, *calls)
	}
	if err := RearmAfterSleep(dir); err != nil || len(*calls) != 2 {
		t.Fatalf("once: %v", *calls)
	}
}
