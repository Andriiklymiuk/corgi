package watch

import (
	"testing"
	"time"
)

func TestARunTheDaemonStoppedIsNotAFailure(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	e := Event{Key: "k1", Workspace: "acme", Ref: "acme/api#7", Kind: KindPRReview}
	l.StartFor(e, now)
	l.Finish("k1", nil, "", "exit status 1", now)
	l.StartFor(e, now)
	l.Finish("k1", nil, "", "exit status 143", now)
	l.StartFor(e, now)
	_ = l.Interrupted(InterruptedReason, now)
	if n := l.FailedInARow("acme", "acme/api#7"); n != 1 {
		t.Errorf("FailedInARow = %d, want 1: a restart that kills a run says nothing about the work", n)
	}
	if n := l.TimesInterrupted("k1"); n != 2 {
		t.Errorf("TimesInterrupted = %d, want 2", n)
	}
}

func TestOnlyATicketRunOwnsThePullRequestsItPrinted(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	link := "https://github.com/acme/api/pull/9"
	l.StartFor(Event{Key: "r1", Workspace: "acme", Ref: "slack-1", Kind: KindReviewRequested}, now)
	l.Finish("r1", []string{link}, "", "", now)
	if _, ok := l.RunThatOpened("acme", link); ok {
		t.Fatal("a review run that printed a colleague's link did not open it")
	}
	l.StartFor(Event{Key: "t1", Workspace: "acme", Ref: "ABC-1", Kind: KindIssueNew}, now)
	l.Finish("t1", []string{link}, "", "", now)
	if r, ok := l.RunThatOpened("acme", link); !ok || r.Ref != "ABC-1" {
		t.Fatalf("RunThatOpened = %+v %v, want the ticket run", r, ok)
	}
}

func TestRunsOnSinceCountsOneKindOnOneRef(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	for i, ref := range []string{"acme/api!5", "acme/api!5", "acme/api!6"} {
		key := "c" + string(rune('a'+i))
		l.StartFor(Event{Key: key, Workspace: "acme", Ref: ref, Kind: KindCIFailed}, now)
		l.Finish(key, nil, "", "", now)
	}
	if n := l.RunsOnSince("acme", "acme/api!5", string(KindCIFailed), now.Add(-time.Hour)); n != 2 {
		t.Errorf("RunsOnSince = %d, want 2", n)
	}
}
