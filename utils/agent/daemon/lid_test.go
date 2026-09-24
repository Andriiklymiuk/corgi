package daemon

import (
	"strings"
	"testing"
	"time"
)

func guardAt(t *testing.T, hours string, start time.Time) *lidGuard {
	t.Helper()
	w, err := ParseQuiet(hours)
	if err != nil {
		t.Fatal(err)
	}
	return &lidGuard{window: w, last: start}
}

func TestALidOpenedByDayRings(t *testing.T) {
	day := time.Date(2026, 9, 24, 14, 0, 0, 0, time.Local)
	g := guardAt(t, "08:00-23:00", day)
	if got := g.observe(day.Add(guardTick), true, true); got != "" {
		t.Fatalf("closing says nothing, got %q", got)
	}
	got := g.observe(day.Add(2*guardTick), false, true)
	if !strings.Contains(got, "lid was opened at 14:00") {
		t.Fatalf("opening by day rings: %q", got)
	}
	if again := g.observe(day.Add(3*guardTick), false, true); again != "" {
		t.Fatalf("an open lid staying open is not news: %q", again)
	}
}

func TestAWakeAfterSleepRingsOnceWithTheSpan(t *testing.T) {
	day := time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)
	g := guardAt(t, "08:00-23:00", day)
	got := g.observe(day.Add(3*time.Hour+12*time.Minute), false, true)
	if !strings.Contains(got, "woke up at 13:12") || !strings.Contains(got, "3 hours 12 minutes asleep") {
		t.Fatalf("a gap between ticks is a sleep: %q", got)
	}
	if again := g.observe(day.Add(3*time.Hour+13*time.Minute), false, true); again != "" {
		t.Fatalf("rang twice: %q", again)
	}
}

func TestAWakeAtNightSaysNothing(t *testing.T) {
	night := time.Date(2026, 9, 24, 2, 0, 0, 0, time.Local)
	g := guardAt(t, "08:00-23:00", night)
	if got := g.observe(night.Add(2*time.Hour), false, true); got != "" {
		t.Fatalf("at night the owner opens it: %q", got)
	}
}

func TestAWakeWithTheLidStillClosedIsNotAnOpening(t *testing.T) {
	day := time.Date(2026, 9, 24, 10, 0, 0, 0, time.Local)
	g := guardAt(t, "08:00-23:00", day)
	g.closed = true
	if got := g.observe(day.Add(time.Hour), true, true); got != "" {
		t.Fatalf("a closed laptop that woke for a moment rings no one: %q", got)
	}
}

func TestNightShiftWorksThroughTheQuietHours(t *testing.T) {
	d := testDaemon(t)
	d.loadWatchFiles()
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), Quiet: "00:00-23:59", NightShift: true}
	if why := fixDeferral(spec, d.watchState.Fixes, time.Now()); why != "" {
		t.Fatalf("night shift defers nothing for quiet hours, got %q", why)
	}
	spec.NightShift = false
	if why := fixDeferral(spec, d.watchState.Fixes, time.Now()); why != "quiet hours" {
		t.Fatalf("without it the quiet hours hold, got %q", why)
	}
}

func TestNightShiftHoldsTheWordUntilMorning(t *testing.T) {
	d := testDaemon(t)
	d.loadWatchFiles()
	var rang []string
	d.Notify = func(_, body string) { rang = append(rang, body) }
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), Quiet: "00:00-23:59", NightShift: true}}
	d.notifyAttentionKey("corgi agent · acme", "fixed ABC-1 - the login", "acme", "", "k1")
	if len(rang) != 0 {
		t.Fatalf("rang at night: %v", rang)
	}
	held := d.watchState.TakeHeld("acme")
	if len(held) != 1 || held[0].Body != "fixed ABC-1 - the login" {
		t.Fatalf("held for the morning: %+v", held)
	}
	d.Watches[0].NightShift = false
	d.notifyAttentionKey("corgi agent · acme", "fixed ABC-2", "acme", "", "k2")
	if len(rang) != 1 {
		t.Fatalf("a plain workspace rings as before: %v", rang)
	}
}
