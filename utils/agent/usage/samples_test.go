package usage

import (
	"testing"
	"time"
)

func TestRecordSampleKeepsOneRowPerFetch(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	l := Limits{FetchedAt: now.Add(-time.Minute), FiveHour: Window{Percent: 40}, SevenDay: Window{Percent: 20}}
	if ok, err := RecordSample(dir, "default", l, now); err != nil || !ok {
		t.Fatalf("first write: ok=%v err=%v", ok, err)
	}
	if ok, _ := RecordSample(dir, "default", l, now.Add(time.Minute)); ok {
		t.Fatal("the same fetch must not be written twice")
	}
	l.FetchedAt = now
	l.FiveHour.Percent = 45
	if ok, _ := RecordSample(dir, "default", l, now.Add(2*time.Minute)); !ok {
		t.Fatal("a newer fetch is a new row")
	}
	got := LoadSamples(SamplesPath(dir, "default"), time.Time{})
	if len(got) != 2 || got[1].FiveHour != 45 {
		t.Fatalf("got %+v", got)
	}
}

func TestForecastProjectsExhaustionAgainstTheReset(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	var samples []Sample
	// 10% per 10 minutes = 60%/h, from 10% at t-30m to 40% now.
	for i := 0; i <= 3; i++ {
		at := now.Add(-time.Duration(30-10*i) * time.Minute)
		samples = append(samples, Sample{At: at, FetchedAt: at, FiveHour: 10 + 10*i, SevenDay: 20})
	}
	l := Limits{FetchedAt: now, FiveHour: Window{Percent: 40, ResetsAt: now.Add(2 * time.Hour)}, SevenDay: Window{Percent: 20, ResetsAt: now.Add(72 * time.Hour)}}
	f := ForecastFrom(samples, l, now)
	if f == nil || f.FiveHour == nil {
		t.Fatal("expected a five-hour forecast")
	}
	if f.FiveHour.PercentPerHour != 60 {
		t.Fatalf("rate %v", f.FiveHour.PercentPerHour)
	}
	// 60% left at 60%/h: one hour, before the two-hour reset → not safe.
	if want := now.Add(time.Hour); !f.FiveHour.ExhaustAt.Equal(want) || f.FiveHour.Safe {
		t.Fatalf("exhaust %v safe %v", f.FiveHour.ExhaustAt, f.FiveHour.Safe)
	}
	if f.SevenDay == nil || !f.SevenDay.Safe || !f.SevenDay.ExhaustAt.IsZero() {
		t.Fatalf("flat week is safe with no exhaustion: %+v", f.SevenDay)
	}
}

func TestForecastNeedsSpread(t *testing.T) {
	now := time.Now()
	samples := []Sample{{FetchedAt: now.Add(-time.Minute), FiveHour: 10}, {FetchedAt: now, FiveHour: 20}}
	if f := ForecastFrom(samples, Limits{}, now); f != nil {
		t.Fatalf("two readings a minute apart say nothing, got %+v", f)
	}
}
