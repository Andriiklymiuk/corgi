package watch

import "testing"

// "Someone reviewed my PR" and "someone wants me to review theirs" were one
// kind, so an unattended run was told to apply review comments on a
// colleague's branch. They are opposites.
func TestAReviewRequestIsNotFeedbackOnMyOwnPR(t *testing.T) {
	mine := Rules{Enabled: true, PRs: true}
	request := Event{Kind: KindReviewRequested, Ref: "acme/api!9", Author: "camille", URL: "https://x/-/merge_requests/9"}

	if mine.Match(request) {
		t.Fatal("--prs is about my pull requests; reviewing someone else's is a different job")
	}
	if why := mine.Why(request); why != "review requests need --reviews" {
		t.Fatalf("the reason has to name the flag: %q", why)
	}

	asked := Rules{Enabled: true, Reviews: true}
	if !asked.Match(request) {
		t.Fatalf("--reviews takes them: %s", asked.Why(request))
	}
	// It is their PR, so Mine is false — and that must not stop it, because a
	// review request is addressed to me by construction.
	if request.Mine {
		t.Fatal("a review request is on someone else's branch")
	}
	if !asked.Match(Event{Kind: KindReviewRequested, Ref: "acme/api!9"}) {
		t.Fatal("no author does not make it someone else's problem")
	}

	// Feedback on my own PR still needs --prs, not --reviews.
	feedback := Event{Kind: KindPRReview, Ref: "acme/api!8", Mine: true, Author: "jarno"}
	if asked.Match(feedback) {
		t.Fatal("--reviews must not silently take feedback on my own PRs")
	}
	if !mine.Match(feedback) {
		t.Fatal("--prs is what asks for feedback on mine")
	}
}
