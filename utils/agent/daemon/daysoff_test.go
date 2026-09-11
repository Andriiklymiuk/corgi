package daemon

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestDaysOffAreParsedAndSleptThrough(t *testing.T) {
	days, err := ParseDaysOff([]string{"weekends"})
	if err != nil || DaysOffWords(days) != "sun, sat" {
		t.Fatalf("weekends = %v %v", days, err)
	}
	days, err = ParseDaysOff([]string{"Mon, fri", "sat"})
	if err != nil || DaysOffWords(days) != "mon, fri, sat" {
		t.Fatalf("mon,fri,sat = %v %v", days, err)
	}
	if _, err := ParseDaysOff([]string{"someday"}); err == nil {
		t.Fatal("a day is a day name")
	}
	if _, err := ParseDaysOff([]string{"mon,tue,wed,thu,fri,sat,sun"}); err == nil {
		t.Fatal("every day off is no watch")
	}
	if days, _ := ParseDaysOff([]string{"none"}); len(days) != 0 {
		t.Fatal("none clears")
	}

	spec := WatchSpec{Workspace: "api", DaysOff: []time.Weekday{time.Saturday, time.Sunday}, Quiet: "23:00-07:00"}
	sat := time.Date(2026, 9, 12, 12, 0, 0, 0, time.Local) // a Saturday noon
	mon := time.Date(2026, 9, 14, 12, 0, 0, 0, time.Local)
	if !dayOff(spec, sat) || dayOff(spec, mon) {
		t.Fatal("saturday is off, monday is not")
	}
	// A day off is quiet all day: nothing rings, no fix starts, the reason
	// is the day, not the hour.
	if !quietNow(spec, sat) || quietNow(spec, mon) {
		t.Fatal("quiet on the day off, not on monday noon")
	}
	if got := fixDeferral(spec, watch.LoadFixLog(t.TempDir()), sat); got != "day off" {
		t.Fatalf("deferral = %q", got)
	}
}
