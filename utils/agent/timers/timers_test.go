package timers

import (
	"os"
	"testing"
	"time"
)

var base = time.Date(2026, 11, 15, 14, 0, 0, 0, time.Local)

func TestParseWhen(t *testing.T) {
	cases := []struct {
		in   []string
		want time.Time
		used int
	}{
		{[]string{"2h"}, base.Add(2 * time.Hour), 1},
		{[]string{"30m", "check"}, base.Add(30 * time.Minute), 1},
		{[]string{"1h30m"}, base.Add(90 * time.Minute), 1},
		{[]string{"3d"}, base.Add(72 * time.Hour), 1},
		{[]string{"17:30"}, time.Date(2026, 11, 15, 17, 30, 0, 0, time.Local), 1},
		{[]string{"9:05"}, time.Date(2026, 11, 16, 9, 5, 0, 0, time.Local), 1},
		{[]string{"tomorrow"}, time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local), 1},
		{[]string{"tomorrow", "10:15", "standup"}, time.Date(2026, 11, 16, 10, 15, 0, 0, time.Local), 2},
	}
	for _, c := range cases {
		got, used, err := ParseWhen(c.in, base)
		if err != nil || !got.Equal(c.want) || used != c.used {
			t.Errorf("%v: %v %d %v, want %v %d", c.in, got, used, err, c.want, c.used)
		}
	}
	for _, bad := range [][]string{{}, {"soon"}, {"25:00"}, {"-1h"}} {
		if _, _, err := ParseWhen(bad, base); err == nil {
			t.Errorf("%v parsed", bad)
		}
	}
}

func TestAddTakeCancel(t *testing.T) {
	dir := t.TempDir()
	a, err := Add(dir, Timer{At: base.Add(time.Hour), Text: "check PR #12", Session: "api"}, base)
	if err != nil || a.ID == "" {
		t.Fatal(a, err)
	}
	b, _ := Add(dir, Timer{At: base.Add(10 * time.Minute), Text: "stand up"}, base)
	c, _ := Add(dir, Timer{At: base.Add(2 * time.Hour), Text: "go home"}, base)
	list, _ := List(dir)
	if len(list) != 3 || list[0].ID != b.ID {
		t.Fatalf("list sorted by time: %+v", list)
	}
	due, _ := TakeDue(dir, base.Add(61*time.Minute))
	if len(due) != 2 {
		t.Fatalf("due: %+v", due)
	}
	if again, _ := TakeDue(dir, base.Add(61*time.Minute)); len(again) != 0 {
		t.Fatal("a timer fires once")
	}
	if _, err := Cancel(dir, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Cancel(dir, c.ID); err == nil {
		t.Fatal("cancelled twice")
	}
	info, _ := os.Stat(Path(dir))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestAddRefuses(t *testing.T) {
	dir := t.TempDir()
	for _, tm := range []Timer{
		{At: base.Add(time.Hour), Text: "  "},
		{At: base.Add(-time.Minute), Text: "late"},
		{At: base.Add(100 * 24 * time.Hour), Text: "far"},
	} {
		if _, err := Add(dir, tm, base); err == nil {
			t.Errorf("accepted %+v", tm)
		}
	}
}

func TestWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		20 * time.Second:          "in under a minute",
		25 * time.Minute:          "in 25 min",
		2 * time.Hour:             "in 2h",
		2*time.Hour + time.Minute: "in 2h 1m",
		5 * 24 * time.Hour:        "in 5 days",
	} {
		if got := Words(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
