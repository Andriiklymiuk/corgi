package watch

import "testing"

// Half of what blocks a day is a person, not a board: the one reviewer you
// are waiting on.
func TestWatchingAPersonRatherThanAProject(t *testing.T) {
	rules := Rules{Enabled: true, PRs: true, Comments: true, From: []string{"max"}}

	waited := Event{Kind: KindPRReview, Ref: "acme/api#7", Mine: true, Author: "Max Mustermann"}
	if !rules.Match(waited) {
		t.Fatalf("the person being waited on gets through: %s", rules.Why(waited))
	}
	// A tracker spells one human several ways.
	for _, spelling := range []string{"max", "MAX", "max@acme.com", "Max M."} {
		if !rules.Match(Event{Kind: KindPRComment, Ref: "acme/api#7", Mine: true, Author: spelling}) {
			t.Errorf("%q is the same person", spelling)
		}
	}

	other := Event{Kind: KindPRReview, Ref: "acme/api#7", Mine: true, Author: "Sam"}
	if rules.Match(other) {
		t.Fatal("everyone else is noise while you wait on one person")
	}
	if why := rules.Why(other); why == "" {
		t.Fatal("it has to say who it was from and who you are waiting on")
	}

	// An author the source did not give cannot match a name.
	if rules.Match(Event{Kind: KindPRReview, Ref: "acme/api#7", Mine: true}) {
		t.Fatal("no author is not a match")
	}

	// Naming nobody is what it always was: anyone.
	anyone := Rules{Enabled: true, PRs: true}
	if !anyone.Match(Event{Kind: KindPRReview, Ref: "acme/api#7", Mine: true, Author: "Sam"}) {
		t.Fatal("without --from every reviewer counts")
	}

	// A fresh ticket is not from anyone in this sense; the filter is for
	// comments and reviews.
	if !rules.Match(Event{Kind: KindIssueNew, Ref: "ABC-1", Mine: true}) {
		t.Fatal("--from must not silently filter new issues")
	}
}
