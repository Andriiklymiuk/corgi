package watch

import (
	"testing"
	"time"
)

func TestADoneMarkHandlesTheRowsBeforeItAndNotAfter(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	older := Event{Key: "linear:HUM-1:c1", Kind: KindIssueComment, Ref: "HUM-1", At: now.Add(-time.Hour)}
	newer := Event{Key: "linear:HUM-1:c2", Kind: KindIssueComment, Ref: "HUM-1", At: now.Add(time.Hour)}
	review := Event{Key: "github:acme/api#7:r1", Kind: KindPRReview, Ref: "acme/api#7", URL: "https://github.com/acme/api/pull/7", At: now.Add(-time.Hour)}
	other := Event{Key: "linear:HUM-2:c1", Kind: KindIssueComment, Ref: "HUM-2", At: now.Add(-time.Hour)}

	l := LoadDoneLog(dir)
	if l.Handled(older) {
		t.Fatal("nothing marked yet")
	}
	if err := l.MarkRef("hum-1", now, "you"); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkRef("https://github.com/acme/api/pull/7", now, "you"); err != nil {
		t.Fatal(err)
	}
	l = LoadDoneLog(dir)
	if !l.Handled(older) {
		t.Fatal("the comment from before the mark is handled, whatever the case of the ref")
	}
	if l.Handled(newer) {
		t.Fatal("a comment after the mark still waits")
	}
	if l.Handled(other) {
		t.Fatal("another ticket is untouched")
	}
	if l.Handled(review) {
		t.Fatal("a link is not a ref; the caller turns it into one")
	}
	if err := l.MarkRef(PullRef(review.URL), now, "you"); err != nil {
		t.Fatal(err)
	}
	if !LoadDoneLog(dir).Handled(review) {
		t.Fatal("the pull request's rows are handled by its ref")
	}
	if err := l.MarkKey(newer.Key, now, "phone"); err != nil {
		t.Fatal(err)
	}
	if !LoadDoneLog(dir).Handled(newer) {
		t.Fatal("one row by its key")
	}
	if InboxHandled(newer, "", nil, nil, LoadDoneLog(dir)) != "done" {
		t.Fatal("the inbox leaves a handled row out")
	}
	if (*DoneLog)(nil).Handled(newer) {
		t.Fatal("no log, nothing handled")
	}
}

func TestMyOwnWordOnAPullRequestAnswersTheReviewRowsBeforeIt(t *testing.T) {
	now := time.Now()
	pulls := &PullLog{Pulls: map[string]PullStatus{"acme/api#7": {State: "open", MyReviewAt: now}}}
	review := Event{Key: "r1", Kind: KindPRReview, Ref: "acme/api#7", At: now.Add(-time.Hour)}
	later := Event{Key: "r2", Kind: KindPRReview, Ref: "acme/api#7", At: now.Add(time.Hour)}
	if got := InboxDone(review, "", pulls, nil); got != "answered" {
		t.Fatalf("a review I wrote after theirs answers it, got %q", got)
	}
	if got := InboxDone(later, "", pulls, nil); got != "" {
		t.Fatalf("a review after my last word waits, got %q", got)
	}
}
