package daemon

import (
	"sync/atomic"
	"time"
)

var handoffWhenAway atomic.Bool

func SetHandoff(on bool) { handoffWhenAway.Store(on) }

func laptopAway(now time.Time) string {
	if !handoffWhenAway.Load() {
		return ""
	}
	m, heat := currentMachine(now)
	if heat != "" {
		return "away-hot"
	}
	if lidClosedNow.Load() && m.Battery >= 0 && !m.Charging {
		return "away-lid"
	}
	return ""
}
