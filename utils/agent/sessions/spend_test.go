package sessions

import (
	"testing"
	"time"
)

func TestParseTokensReadsWhatAPersonTypes(t *testing.T) {
	for in, want := range map[string]int64{"50M": 50_000_000, "800k": 800_000, "2B": 2_000_000_000, "1.5m": 1_500_000, "4200": 4200} {
		got, err := ParseTokens(in)
		if err != nil || got != want {
			t.Fatalf("%s: %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "lots", "-3M", "0"} {
		if _, err := ParseTokens(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if Tokens(52_300_000) != "52.3M" || Tokens(980_000) != "980k" || Tokens(412) != "412" {
		t.Fatal("the board's one word for a count")
	}
}

// A session passes its budget once: the sweep rings then, and the row
// says over budget until the budget moves.
func TestSpendCrossesABudgetOnce(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("UserPromptSubmit", "s1", 0))
	now := time.Now()
	if _, crossed := r.SetSpend("s1", Spend{Tokens: 40_000_000, At: now}, 50_000_000); crossed {
		t.Fatal("under budget")
	}
	changed, crossed := r.SetSpend("s1", Spend{Tokens: 51_000_000, At: now}, 50_000_000)
	if !changed || !crossed {
		t.Fatalf("passing the budget rings: %v %v", changed, crossed)
	}
	if _, crossed := r.SetSpend("s1", Spend{Tokens: 52_000_000, At: now}, 50_000_000); crossed {
		t.Fatal("once")
	}
	s, _ := r.Lookup("s1")
	if !s.OverCap || s.Spend.Tokens != 52_000_000 {
		t.Fatalf("the row says so: %+v", s)
	}
	// Its own, bigger budget takes it back under; the default no longer counts.
	if err := r.SetCap("s1", 100_000_000); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Lookup("s1")
	if s.OverCap || s.Cap != 100_000_000 {
		t.Fatalf("its own budget wins: %+v", s)
	}
	if _, crossed := r.SetSpend("s1", Spend{Tokens: 60_000_000, At: now}, 50_000_000); crossed {
		t.Fatal("under its own budget")
	}
	slots := r.Snapshot(time.Now()).Slots
	found := false
	for _, sl := range slots {
		if sl.SessionID == "s1" {
			found = sl.Spend == "60.0M" && !sl.OverCap
		}
	}
	if !found {
		t.Fatalf("the key says what it spent: %+v", slots)
	}
}
