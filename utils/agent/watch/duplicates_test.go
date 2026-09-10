package watch

import "testing"

// A ticket already closed as a duplicate is the one piece of work that is
// certainly wasted, whichever tracker it came from.
func TestADuplicateOrCancelledTicketIsNotWorthAnyonesTime(t *testing.T) {
	rules := Rules{Enabled: true}
	for _, state := range []string{"Duplicate", "duplicate", "Canceled", "Cancelled", "Won't do", "Not planned", "  REJECTED  "} {
		e := Event{Kind: KindIssueNew, Ref: "ABC-1", State: state, Mine: true}
		if rules.Match(e) {
			t.Errorf("state %q should stop the event", state)
		}
		if why := rules.Why(e); why == "" {
			t.Errorf("state %q must say why it stopped", state)
		}
	}
	for _, state := range []string{"In Progress", "Ready", "Backlog", "Duplicate detection", ""} {
		if !rules.Match(Event{Kind: KindIssueNew, Ref: "ABC-1", State: state, Mine: true}) {
			t.Errorf("state %q is normal work", state)
		}
	}
}

// Asking for a column means you meant it, even a dead-looking one.
func TestNamedStatesBeatTheDuplicateRule(t *testing.T) {
	rules := Rules{Enabled: true, States: []string{"Duplicate"}}
	if !rules.Match(Event{Kind: KindIssueNew, Ref: "ABC-1", State: "Duplicate", Mine: true}) {
		t.Fatal("--states Duplicate is a deliberate ask")
	}
}

func TestSeveralCommentsOnOnePRAreOneThingToLookAt(t *testing.T) {
	s := &State{}
	pr := Event{Kind: KindPRComment, Ref: "acme/api#7", Mine: true}

	if n := s.SameRefThisRound("api", pr); n != 0 {
		t.Fatalf("the first one is news: %d", n)
	}
	if n := s.SameRefThisRound("api", pr); n != 1 {
		t.Fatalf("the second is the same pull request: %d", n)
	}
	if n := s.SameRefThisRound("api", Event{Kind: KindPRReview, Ref: "acme/api#7"}); n != 2 {
		t.Fatalf("a review on the same PR is still that PR: %d", n)
	}

	// Another workspace's PR that happens to share a ref is not the same PR.
	if n := s.SameRefThisRound("web", pr); n != 0 {
		t.Fatalf("refs are per workspace: %d", n)
	}

	// A fresh ticket is one event by construction; never collapsed.
	issue := Event{Kind: KindIssueNew, Ref: "ABC-1"}
	if n := s.SameRefThisRound("api", issue); n != 0 {
		t.Fatalf("issues are not collapsed: %d", n)
	}
	if n := s.SameRefThisRound("api", issue); n != 0 {
		t.Fatalf("issues are never collapsed: %d", n)
	}

	// The next poll is a new day for that pull request.
	s.NewRound()
	if n := s.SameRefThisRound("api", pr); n != 0 {
		t.Fatalf("a comment next round is news again: %d", n)
	}
}
