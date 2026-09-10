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

// The real case: a comment arrived on a ticket that was already Done, and
// the rules had no way to know — comment events carried no column at all.
func TestACommentOnFinishedWorkIsNotWork(t *testing.T) {
	rules := Rules{Enabled: true, Comments: true}

	for _, state := range []string{"Done", "done", "Closed", "Resolved", "Completed", "Duplicate", "Cancelled"} {
		e := Event{Kind: KindIssueComment, Ref: "ABC-1", State: state, Mine: true, Author: "sam"}
		if rules.Match(e) {
			t.Errorf("a comment on a %q issue is chatter", state)
		}
		if why := rules.Why(e); why == "" {
			t.Errorf("state %q must say why it stopped", state)
		}
	}

	// Work still in flight is exactly what comments are for.
	for _, state := range []string{"In Progress", "In Review", "Ready", ""} {
		if !rules.Match(Event{Kind: KindIssueComment, Ref: "ABC-1", State: state, Mine: true}) {
			t.Errorf("a comment on a %q issue is the point of --comments", state)
		}
	}

	// Naming the column means you meant it, even a finished one.
	asked := Rules{Enabled: true, Comments: true, States: []string{"Done"}}
	if !asked.Match(Event{Kind: KindIssueComment, Ref: "ABC-1", State: "Done", Mine: true}) {
		t.Fatal("--states Done is a deliberate ask")
	}

	// A review on a merged PR is still feedback on my code; only tracker
	// comments carry an issue column.
	pr := Rules{Enabled: true, PRs: true}
	if !pr.Match(Event{Kind: KindPRReview, Ref: "acme/api#7", State: "merged", Mine: true}) {
		t.Fatal("a PR's own state is not an issue column")
	}
}
