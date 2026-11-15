package daemon

import (
	"testing"
	"time"
)

func TestHandoffOnlyWhenAskedAndAway(t *testing.T) {
	t.Cleanup(func() {
		SetHandoff(false)
		lidClosedNow.Store(false)
		resetMachine()
	})
	now := time.Now()
	machine.mu.Lock()
	machine.checked, machine.m, machine.heat = now, Machine{Battery: 60, Charging: false, Known: true}, ""
	machine.mu.Unlock()
	lidClosedNow.Store(true)
	if why := laptopAway(now); why != "" {
		t.Fatalf("off by default, got %q", why)
	}
	SetHandoff(true)
	if why := laptopAway(now); why != "away-lid" {
		t.Fatalf("lid closed on battery: %q", why)
	}
	machine.mu.Lock()
	machine.m.Charging = true
	machine.mu.Unlock()
	if why := laptopAway(now); why != "" {
		t.Fatalf("closed lid on the charger keeps the lead: %q", why)
	}
	machine.mu.Lock()
	machine.heat = "hot"
	machine.mu.Unlock()
	if why := laptopAway(now); why != "away-hot" {
		t.Fatalf("too hot hands over: %q", why)
	}
	if why := laptopUnwell(WatchSpec{}, nil, now); why != "away-hot" {
		t.Fatalf("peers hear it: %q", why)
	}
}
