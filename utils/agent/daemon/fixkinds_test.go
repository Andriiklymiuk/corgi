package daemon

import (
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestFixKindsSplitUnattendedWorkByKind(t *testing.T) {
	everything := WatchSpec{Action: "fix"}
	for _, k := range []watch.Kind{watch.KindIssueNew, watch.KindIssueComment, watch.KindPRComment, watch.KindPRReview} {
		if !everything.FixesKind(k) {
			t.Fatalf("fix with no kinds named is what it always was — everything: %s", k)
		}
	}

	reportOnly := WatchSpec{Action: "notify", FixKinds: []string{"pr.review"}}
	if reportOnly.FixesKind(watch.KindPRReview) {
		t.Fatal("naming kinds must not turn a reporting watch into an unattended one")
	}

	// The ask: work review comments on your own, but only be told about a
	// fresh ticket.
	reviewsOnly := WatchSpec{Action: "fix", FixKinds: []string{"pr.review", "pr.comment"}}
	if !reviewsOnly.FixesKind(watch.KindPRReview) || !reviewsOnly.FixesKind(watch.KindPRComment) {
		t.Fatal("a named kind is worked on")
	}
	if reviewsOnly.FixesKind(watch.KindIssueNew) {
		t.Fatal("a kind that was not named is only reported")
	}

	if !(WatchSpec{Action: "fix", FixKinds: []string{" PR.Review "}}).FixesKind(watch.KindPRReview) {
		t.Fatal("a hand-written config with spacing or capitals still means the kind")
	}
}
