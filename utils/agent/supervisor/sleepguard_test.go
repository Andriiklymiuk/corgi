package supervisor

import (
	"strings"
	"testing"
)

const customOutput = `Battery Power:
 standby              1
 sleep                1
 displaysleep         60
AC Power:
 standby              1
 sleep                0
 displaysleep         10
`

func TestSleepRiskOnBatteryWithSleepEnabled(t *testing.T) {
	risk := sleepRiskFrom("Now drawing from 'Battery Power'\n", customOutput)
	if !risk.AtRisk() {
		t.Fatal("a Mac that sleeps in a minute on battery is exactly the risk to report")
	}
	if !risk.OnBattery || risk.SleepMinutes != 1 {
		t.Errorf("risk = %+v", risk)
	}
	if !strings.Contains(risk.Reason, "1 minute") || strings.Contains(risk.Reason, "1 minutes") {
		t.Errorf("reason must read naturally: %q", risk.Reason)
	}
	if !strings.Contains(risk.Fix, "pmset -b sleep 0") {
		t.Errorf("fix must be the command that removes it: %q", risk.Fix)
	}
}

func TestNoRiskOnACPower(t *testing.T) {
	risk := sleepRiskFrom("Now drawing from 'AC Power'\n", customOutput)
	if risk.AtRisk() {
		t.Errorf("the AC block has sleep 0, so there is nothing to warn about: %+v", risk)
	}
	if risk.OnBattery {
		t.Error("AC power must not read as battery")
	}
}

func TestNoRiskWhenBatterySleepIsDisabled(t *testing.T) {
	custom := "Battery Power:\n sleep                0\nAC Power:\n sleep                0\n"
	if risk := sleepRiskFrom("Now drawing from 'Battery Power'\n", custom); risk.AtRisk() {
		t.Errorf("sleep 0 on battery is the fixed state, not a risk: %+v", risk)
	}
}

func TestUnreadablePowerStateIsNotARisk(t *testing.T) {
	risk := sleepRiskFrom("", "")
	if risk.AtRisk() {
		t.Error("a warning on every status is worse than a missing one")
	}
	if risk.SleepMinutes != -1 {
		t.Errorf("an unread setting is -1, got %d", risk.SleepMinutes)
	}
}

func TestBatteryReadsItsOwnBlockNotTheACOne(t *testing.T) {
	// The AC block says 0; the battery block says 5. Reading the wrong one
	// would silently clear the warning on the machine that needs it.
	custom := "Battery Power:\n sleep                5\nAC Power:\n sleep                0\n"
	risk := sleepRiskFrom("Now drawing from 'Battery Power'\n", custom)
	if risk.SleepMinutes != 5 || !risk.AtRisk() {
		t.Errorf("risk = %+v", risk)
	}
	if !strings.Contains(risk.Reason, "5 minutes") {
		t.Errorf("reason = %q", risk.Reason)
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "minute"); got != "1 minute" {
		t.Errorf("got %q", got)
	}
	if got := plural(3, "minute"); got != "3 minutes" {
		t.Errorf("got %q", got)
	}
}
