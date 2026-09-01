package supervisor

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// `caffeinate -s` (PreventSystemSleep) is honoured on AC power only — that is
// documented in `man caffeinate`. `-i` (PreventUserIdleSystemSleep) is NOT: it
// holds off idle sleep on battery too, and corgi passes both. So a laptop on
// battery does not doze off mid-session; what still ends the session is a
// closed lid or a flat battery, neither of which any assertion can stop.
type SleepRisk struct {
	OnBattery bool
	// SleepMinutes is 0 for never, negative when it could not be read.
	SleepMinutes int
	Reason       string
	Fix          string
}

func (s SleepRisk) AtRisk() bool { return s.Reason != "" }

// Best-effort: anything unreadable is reported as no risk, because a false
// warning on every status is worse than a missing one.
func CheckSleepRisk() SleepRisk {
	if runtime.GOOS != "darwin" {
		return SleepRisk{SleepMinutes: -1}
	}
	return sleepRiskFrom(runCommand("pmset", "-g", "batt"), runCommand("pmset", "-g", "custom"))
}

// Bounded: a hung pmset must not hang `corgi agent status`.
func runCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

var sleepSettingPattern = regexp.MustCompile(`(?m)^\s*sleep\s+(\d+)`)

func sleepRiskFrom(battOutput, customOutput string) SleepRisk {
	risk := SleepRisk{SleepMinutes: -1}
	if battOutput == "" && customOutput == "" {
		return risk
	}
	risk.OnBattery = strings.Contains(battOutput, "'Battery Power'")

	section := customOutput
	if risk.OnBattery {
		// Only the active power source's block decides what happens now.
		if i := strings.Index(customOutput, "Battery Power:"); i >= 0 {
			section = customOutput[i:]
			if j := strings.Index(section, "AC Power:"); j > 0 {
				section = section[:j]
			}
		}
	} else if i := strings.Index(customOutput, "AC Power:"); i >= 0 {
		section = customOutput[i:]
	}

	if m := sleepSettingPattern.FindStringSubmatch(section); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			risk.SleepMinutes = v
		}
	}
	if !risk.OnBattery || risk.SleepMinutes <= 0 {
		return risk
	}

	risk.Reason = "on battery (idle sleep is set to " + plural(risk.SleepMinutes, "minute") +
		"). The wake lock holds idle sleep off, but a closed lid or a flat battery still ends a remote session"
	risk.Fix = "plug in and keep the lid open for a long unattended run"
	return risk
}

func plural(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}
