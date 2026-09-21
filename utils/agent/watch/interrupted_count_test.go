package watch

import (
	"testing"
	"time"
)

func TestAnInterruptedRunDoesNotSpendTheCaps(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	l.Start("acme", "acme/api#1", now.Add(-30*time.Minute))
	l.Start("acme", "acme/api#2", now.Add(-20*time.Minute))
	keys := l.Interrupted(InterruptedReason, now.Add(-19*time.Minute))
	if len(keys) != 2 {
		t.Fatalf("both were running: %v", keys)
	}
	if n := l.StartedSince("acme", now.Add(-time.Hour)); n != 0 {
		t.Fatalf("a run the daemon stopped did no work and costs no cap: %d", n)
	}
	l.Start("acme", "acme/api#3", now.Add(-10*time.Minute))
	if n := l.StartedSince("acme", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("a real start counts: %d", n)
	}
}
