package watch

import (
	"testing"
	"time"
)

func TestSchedulesParseAndComeDue(t *testing.T) {
	loc := time.Local
	mon0900 := time.Date(2026, 9, 14, 9, 0, 0, 0, loc) // a Monday
	for text, want := range map[string]string{"daily 08:30": "daily 08:30", "every 2h": "every 2h0m0s", "weekly mon 09:00": "weekly Mon 09:00", "Weekly Friday 16:00": "weekly Fri 16:00"} {
		s, err := ParseSchedule(text)
		if err != nil || s.String() != want {
			t.Errorf("%q: %v %q", text, err, s.String())
		}
	}
	for _, bad := range []string{"", "hourly", "daily 25:00", "every 1m", "weekly 09:00"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
	daily, _ := ParseSchedule("daily 08:30")
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, loc)
	if daily.Due(time.Time{}, day.Add(8*time.Hour)) {
		t.Fatal("not before the time")
	}
	if !daily.Due(time.Time{}, day.Add(9*time.Hour)) {
		t.Fatal("due after the time when it never ran")
	}
	if daily.Due(day.Add(9*time.Hour), day.Add(10*time.Hour)) {
		t.Fatal("not twice in a day")
	}
	if !daily.Due(day.Add(9*time.Hour), day.Add(24*time.Hour+9*time.Hour)) {
		t.Fatal("due the next day")
	}
	every, _ := ParseSchedule("every 2h")
	if every.Due(mon0900, mon0900.Add(time.Hour)) || !every.Due(mon0900, mon0900.Add(2*time.Hour)) {
		t.Fatal("every 2h")
	}
	weekly, _ := ParseSchedule("weekly Mon 09:00")
	if !weekly.Due(time.Time{}, mon0900) || weekly.Due(mon0900, mon0900.Add(24*time.Hour)) || weekly.Due(time.Time{}, mon0900.Add(24*time.Hour)) {
		t.Fatal("weekly on the day, once")
	}
	if _, ok := CatalogKind("Babysit-PR"); !ok {
		t.Fatal("catalog lookup is case-insensitive")
	}
}

// A routine's report is the newest one per routine, for a day; other rows
// are untouched.
func TestRoutineReportsAgeOutOfTheInbox(t *testing.T) {
	now := time.Now()
	k := NewInboxKeeper(now)
	newest := Event{Kind: KindRoutine, Ref: "routine/digest", At: now.Add(-time.Hour)}
	older := Event{Kind: KindRoutine, Ref: "routine/digest", At: now.Add(-3 * time.Hour)}
	stale := Event{Kind: KindRoutine, Ref: "routine/deps", At: now.Add(-30 * time.Hour)}
	issue := Event{Kind: KindIssueNew, Ref: "ABC-1", At: now.Add(-100 * time.Hour)}
	if !k.Keep(newest) || k.Keep(older) || k.Keep(stale) || !k.Keep(issue) {
		t.Fatal("newest per routine within a day; issues always")
	}
}
