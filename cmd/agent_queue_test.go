package cmd

import "testing"

// One queue, not one per repo: whoever is blocked comes first.
func TestTheInboxIsOrderedByWhoIsStuck(t *testing.T) {
	order := []string{"review.requested", "ci.failed", "pr.review", "pr.comment", "issue.comment", "issue.new"}
	for i := 1; i < len(order); i++ {
		if waitingRank(order[i-1]) > waitingRank(order[i]) {
			t.Fatalf("%s must not outrank %s", order[i], order[i-1])
		}
	}
	if waitingRank("review.requested") >= waitingRank("issue.new") {
		t.Fatal("a colleague waiting on you outranks your own backlog")
	}
	if waitingRank("pr.review") != waitingRank("pr.comment") {
		t.Fatal("both are someone replying on your own PR")
	}
	if waitingRank("something.new") != waitingRank("issue.new") {
		t.Fatal("an unknown kind blocks nobody, same as a fresh ticket")
	}
}
