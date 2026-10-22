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

func TestAMachineFailureIsNotTheWorks(t *testing.T) {
	for out, want := range map[string]string{
		"Failed to authenticate: OAuth session expired and could not be refreshed": "the Claude login expired",
		"API Error: Can't reach the API server - check your internet or DNS (ENOTFOUND)": "no internet",
		"You've hit your session limit · resets 2:50am (Europe/Berlin)":                "the account hit its limit",
		"Tests: 2 failed, 10564 passed":                                                "",
	} {
		if got := EnvironmentFailure(out); got != want {
			t.Errorf("EnvironmentFailure(%q) = %q, want %q", out, got, want)
		}
	}
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	e := Event{Key: "d", Workspace: "acme", Ref: "routine/digest", Kind: KindRoutine}
	for i := 0; i < 3; i++ {
		l.StartFor(e, now)
		l.Finish("d", nil, "", EnvironmentPrefix+"no internet", now)
	}
	if n := l.FailedInARow("acme", "routine/digest"); n != 0 {
		t.Errorf("FailedInARow = %d: offline mornings never trip the breaker", n)
	}
	if n := l.EnvironmentFailures("acme", "routine/digest"); n != 3 {
		t.Errorf("EnvironmentFailures = %d, want 3", n)
	}
}

func TestAPullLinkLosesItsTab(t *testing.T) {
	for in, want := range map[string]string{
		"https://gitlab.com/acme/core/-/merge_requests/9134/pipelines": "https://gitlab.com/acme/core/-/merge_requests/9134",
		"https://github.com/acme/api/pull/7/files#diff-1":              "https://github.com/acme/api/pull/7",
		"https://github.com/acme/api/pull/7":                           "https://github.com/acme/api/pull/7",
	} {
		if got := PullLink(in); got != want {
			t.Errorf("PullLink(%q) = %q", in, got)
		}
	}
}
