package sendgate

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func clockAt(t time.Time) (*time.Time, func() time.Time) {
	now := t
	return &now, func() time.Time { return now }
}

func TestARetryStormStopsAtTheCap(t *testing.T) {
	g := InMemory()
	now, clock := clockAt(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	g.Now = clock
	sent := 0
	for i := 0; i < 10000; i++ {
		if g.Allow("slack", fmt.Sprintf("C1/%d", i%500), fmt.Sprintf("Could not do this: overloaded (%d)", i)) == nil {
			sent++
		}
		*now = now.Add(time.Second)
	}
	// 10000 seconds is under three hours: three windows of 60.
	if sent > 3*Default.PerFamily {
		t.Fatalf("%d sends got through a retry storm; the cap is %d an hour", sent, Default.PerFamily)
	}
}

func TestTheDayCapHoldsAcrossWindows(t *testing.T) {
	g := InMemory()
	now, clock := clockAt(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	g.Now = clock
	sent := 0
	for i := 0; i < 24*60; i++ {
		if g.Allow("slack", fmt.Sprintf("C%d", i%50), fmt.Sprint(i)) == nil {
			sent++
		}
		*now = now.Add(time.Minute)
	}
	if sent != Default.PerDay {
		t.Fatalf("sent %d in a day, want the cap of %d", sent, Default.PerDay)
	}
}

func TestTheSameMessageToTheSamePlaceGoesOnce(t *testing.T) {
	g := InMemory()
	if err := g.Allow("slack", "C1/1.2", "Could not do this: 529."); err != nil {
		t.Fatal(err)
	}
	if err := g.Allow("slack", "C1/1.2", "Could not do this: 529."); !errors.Is(err, ErrThrottled) {
		t.Fatalf("a repeat must be held back, got %v", err)
	}
	if err := g.Allow("slack", "C2/1.2", "Could not do this: 529."); err != nil {
		t.Fatalf("another thread is another message: %v", err)
	}
}

func TestOneTargetCannotTakeTheWholeBudget(t *testing.T) {
	g := InMemory()
	for i := 0; i < Default.PerTarget; i++ {
		if err := g.Allow("slack", "C1/1", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Allow("slack", "C1/1", "one more"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("over the per-target cap must be held back, got %v", err)
	}
	if err := g.Allow("slack", "C2/1", "elsewhere"); err != nil {
		t.Fatalf("another target still has room: %v", err)
	}
}

func TestAPauseHoldsEveryTargetAndIsClamped(t *testing.T) {
	g := InMemory()
	now, clock := clockAt(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	g.Now = clock
	g.Pause("slack", 0)
	if err := g.Allow("slack", "C9", "hi"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("a paused family must hold back, got %v", err)
	}
	if err := g.Allow("webhook", "slack:abc", "hi"); err != nil {
		t.Fatalf("a pause is per family: %v", err)
	}
	*now = now.Add(Default.MinPause)
	if err := g.Allow("slack", "C9", "hi"); err != nil {
		t.Fatalf("the pause must end: %v", err)
	}
	g.Pause("slack", 100*24*time.Hour)
	*now = now.Add(Default.MaxPause)
	if err := g.Allow("slack", "C9", "later"); err != nil {
		t.Fatalf("a huge Retry-After is clamped to %s: %v", Default.MaxPause, err)
	}
}

func TestTheLedgerIsSharedThroughTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch", "sends.json")
	a, b := New(path), New(path)
	if err := a.Allow("slack", "C1/1", "same"); err != nil {
		t.Fatal(err)
	}
	if err := b.Allow("slack", "C1/1", "same"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("a second process must see the first one's send, got %v", err)
	}
	a.Pause("slack", time.Minute)
	if err := b.Allow("slack", "C2/1", "other"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("a second process must see the pause, got %v", err)
	}
}
