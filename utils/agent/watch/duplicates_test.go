package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

	// A pull request carries its own state, and merged is finished there too:
	// see TestACommentOnAMergedPullRequestIsNotWork.
	pr := Rules{Enabled: true, PRs: true}
	if !pr.Match(Event{Kind: KindPRReview, Ref: "acme/api#7", State: "open", Mine: true}) {
		t.Fatal("an open pull request is live work")
	}
}

// Ignore is a person saying no thanks. Seen is corgi saying it told you —
// every delivered event is seen, so the inbox cannot filter on that.
func TestIgnoringIsNotTheSameAsHavingBeenSeen(t *testing.T) {
	dir := t.TempDir()
	s := LoadState(dir)

	s.MarkSeen("jira:ABC-1")
	if s.IsIgnored("jira:ABC-1") {
		t.Fatal("delivering an event is not dismissing it — this is what emptied the inbox")
	}

	if err := s.Ignore("jira:ABC-1"); err != nil {
		t.Fatal(err)
	}
	if !s.IsIgnored("jira:ABC-1") {
		t.Fatal("a dismissed event stays dismissed")
	}
	if !LoadState(dir).IsIgnored("jira:ABC-1") {
		t.Fatal("it has to survive a restart, or it comes back tomorrow")
	}
	if err := s.Ignore("jira:ABC-1"); err != nil {
		t.Fatal("ignoring twice is not an error")
	}

	// Undoing a run puts it back in front of you.
	if err := LoadState(dir).Unignore("jira:ABC-1"); err != nil {
		t.Fatal(err)
	}
	if LoadState(dir).IsIgnored("jira:ABC-1") {
		t.Fatal("undo has to put the event back in the inbox")
	}
}

// A run that stops takes its context with it unless it writes something down.
func TestARunLeavesSomethingForTheNextOne(t *testing.T) {
	dir := t.TempDir()
	log := LoadFixLog(dir)
	e := Event{Key: "jira:ABC-1", Workspace: "api", Ref: "ABC-1"}
	now := time.Now()

	log.StartFor(e, now.Add(-time.Hour))
	log.Finish("jira:ABC-1", nil, "", "timed out", now.Add(-30*time.Minute))
	log.SetHandover("jira:ABC-1", "Found it in registry.go; the fix needs the migration first.", now)

	if got := log.LastHandover("api", "ABC-1"); !strings.Contains(got, "registry.go") {
		t.Fatalf("the next attempt starts from here: %q", got)
	}
	if got := log.LastHandover("api", "OTHER-1"); got != "" {
		t.Fatalf("notes belong to their own ticket: %q", got)
	}
	if got := log.LastHandover("web", "ABC-1"); got != "" {
		t.Fatalf("and to their own workspace: %q", got)
	}
	if !strings.Contains(LoadFixLog(dir).LastHandover("api", "ABC-1"), "registry.go") {
		t.Fatal("it has to survive the daemon that wrote it")
	}

	// The tail is what a session says at the end, blank lines dropped.
	if got := TailLines("start\n\nmiddle\n\n  last  \n\n", 2); got != "middle\nlast" {
		t.Fatalf("tail = %q", got)
	}
	if TailLines("", 3) != "" {
		t.Fatal("no output, nothing to hand over")
	}
}

// Ten comment fixes and ten whole tickets are the same count and nowhere near
// the same spend, so the cap has to be able to talk about spend.
func TestWhatARunCostsIsRemembered(t *testing.T) {
	dir := t.TempDir()
	log := LoadFixLog(dir)
	now := time.Now()
	for i, pct := range []int{2, 30, 4} {
		key := "k" + string(rune('a'+i))
		log.StartFor(Event{Key: key, Workspace: "api", Ref: key}, now)
		log.Finish(key, nil, "", "", now)
		log.SetSpent(key, pct)
	}
	// The median, so one runaway run does not make every later one look
	// unaffordable.
	if got := log.TypicalSpend("api"); got != 4 {
		t.Fatalf("typical spend = %d, want the median 4", got)
	}
	if got := log.TypicalSpend("web"); got != 0 {
		t.Fatalf("a workspace with nothing measured has no figure: %d", got)
	}

	// A window that reset mid-run reads as a negative; that is not a receipt.
	log.StartFor(Event{Key: "kz", Workspace: "web", Ref: "kz"}, now)
	log.Finish("kz", nil, "", "", now)
	log.SetSpent("kz", -40)
	if got := log.TypicalSpend("web"); got != 0 {
		t.Fatalf("a nonsense figure must not be kept: %d", got)
	}
}

// A comment on a pull request that is already merged reads exactly like one
// on live work, because a notification says nothing about the state.
func TestACommentOnAMergedPullRequestIsNotWork(t *testing.T) {
	rules := Rules{Enabled: true, PRs: true, Reviews: true}

	for _, state := range []string{"merged", "closed", "Merged", "locked"} {
		e := Event{Kind: KindPRComment, Ref: "acme/api#7", Mine: true, State: state, Author: "sam"}
		if rules.Match(e) {
			t.Errorf("a comment on a %q pull request is chatter", state)
		}
		if why := rules.Why(e); !strings.Contains(why, "not work") {
			t.Errorf("state %q must say why: %q", state, why)
		}
		// Nobody needs to review something already merged either.
		req := Event{Kind: KindReviewRequested, Ref: "acme/api#7", State: state}
		if rules.Match(req) {
			t.Errorf("a review request on a %q pull request is nothing to do", state)
		}
	}

	// Live work is exactly what these are for. An empty state means the
	// source could not say, and guessing it closed would swallow real
	// feedback.
	for _, state := range []string{"open", "opened", "draft", ""} {
		if !rules.Match(Event{Kind: KindPRComment, Ref: "acme/api#7", Mine: true, State: state}) {
			t.Errorf("a comment on a %q pull request is the point of --prs", state)
		}
		if !rules.Match(Event{Kind: KindReviewRequested, Ref: "acme/api#7", State: state}) {
			t.Errorf("a review request on a %q pull request is a real ask", state)
		}
	}

	// A review on my own merged PR is also over.
	if rules.Match(Event{Kind: KindPRReview, Ref: "acme/api#7", Mine: true, State: "merged"}) {
		t.Fatal("feedback arrives after a merge; acting on it unattended does not")
	}
}

// The daemon keeps its State in memory from start; the phone loads a fresh
// one to ignore a row. Both write the same file whole, so the daemon's next
// save used to bring the row back — and the daemon never learned it was
// ignored, so the unattended mode could still pick it up.
func TestAnIgnoreFromAnotherProcessSurvivesTheDaemonsNextSave(t *testing.T) {
	dir := t.TempDir()
	daemon := LoadState(dir)
	phone := LoadState(dir)

	if err := phone.Ignore("jira:ABC-1"); err != nil {
		t.Fatal(err)
	}
	daemon.MarkSeen("jira:XYZ-9") // any save from the stale copy

	if !LoadState(dir).IsIgnored("jira:ABC-1") {
		t.Fatal("the daemon's save clobbered the phone's ignore")
	}
	if !daemon.IsIgnored("jira:ABC-1") {
		t.Fatal("the daemon has to see an ignore it did not make, or it fixes the ticket anyway")
	}
}

// An ignored list written by an older corgi lives inside state.json; it has
// to move to its own file, once, and not come back.
func TestOldIgnoredListMovesOutOfStateJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "watch"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"cursors":{},"seen":["jira:ABC-1"],"ignored":["jira:ABC-1"]}`
	if err := os.WriteFile(filepath.Join(dir, "watch", "state.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	s := LoadState(dir)
	if !s.IsIgnored("jira:ABC-1") {
		t.Fatal("an ignore from before the move still counts")
	}
	s.MarkSeen("jira:XYZ-9")
	data, _ := os.ReadFile(filepath.Join(dir, "watch", "state.json"))
	if strings.Contains(string(data), "ignored") {
		t.Fatalf("state.json still carries the list, so a stale copy can clobber it: %s", data)
	}
	if !LoadState(dir).IsIgnored("jira:ABC-1") {
		t.Fatal("the move lost the ignore")
	}
}

// Two failed runs in a row on one ticket trip the breaker; a person
// unblocks, and the runs before that no longer count toward the next trip.
func TestTheBreakerCountsFailuresPerTicket(t *testing.T) {
	dir := t.TempDir()
	l := LoadFixLog(dir)
	now := time.Now()
	e := Event{Key: "k1", Ref: "ABC-1", Workspace: "api"}
	l.StartFor(e, now)
	l.Finish("k1", nil, "", "build failed", now)
	if l.FailedInARow("api", "ABC-1") != 1 {
		t.Fatal("one failure")
	}
	l.StartFor(Event{Key: "k1b", Ref: "ABC-1", Workspace: "api"}, now)
	l.Finish("k1b", nil, "", "not started: 3/h cap", now)
	if l.FailedInARow("api", "ABC-1") != 1 {
		t.Fatal("a run that did not start is not a failure")
	}
	l.StartFor(Event{Key: "k1c", Ref: "ABC-1", Workspace: "api"}, now)
	l.Finish("k1c", nil, "", "tests red", now)
	if l.FailedInARow("api", "ABC-1") != BreakerAfter {
		t.Fatalf("two: %d", l.FailedInARow("api", "ABC-1"))
	}
	if l.FailedInARow("api", "ABC-2") != 0 {
		t.Fatal("another ticket is untouched")
	}
	l.Block("api", "ABC-1", "tests red", BlockedByBreaker, now)
	if b, ok := LoadFixLog(dir).Blocked("api", "ABC-1"); !ok || b.By != BlockedByBreaker || b.Reason != "tests red" {
		t.Fatalf("blocked survives a restart: %+v %v", b, ok)
	}
	if !LoadFixLog(dir).Unblock("api", "ABC-1") {
		t.Fatal("unblocked")
	}
	l = LoadFixLog(dir)
	if _, ok := l.Blocked("api", "ABC-1"); ok || l.FailedInARow("api", "ABC-1") != 0 {
		t.Fatal("an unblock forgives the runs before it")
	}
	l.StartFor(Event{Key: "k1d", Ref: "ABC-1", Workspace: "api"}, now)
	l.Finish("k1d", []string{"https://github.com/a/b/pull/1"}, "", "", now)
	if l.FailedInARow("api", "ABC-1") != 0 {
		t.Fatal("a run that opened a PR clears the count")
	}
}

// "Thanks! test is ok" is the end of the work, not more of it: no buzz, no
// fix. A question, a request or a complaint in the same length still is.
func TestAThankYouIsNotWork(t *testing.T) {
	for body, ack := range map[string]bool{
		"Thanks   ! test is ok":                 true,
		"LGTM 👍":                                true,
		"works on staging, thank you":           true,
		"✅":                                     true,
		"Thanks! But the banner is still wrong": false,
		"Thanks, could you also add the docs?":  false,
		"test is ok on staging, fails on prod":  false,
		"":                                      false,
		"any update on this?":                   false,
	} {
		if IsAcknowledgement(body) != ack {
			t.Errorf("%q: ack=%v, want %v", body, IsAcknowledgement(body), ack)
		}
	}
	rules := Rules{Enabled: true, Comments: true}
	e := Event{Kind: KindIssueComment, Ref: "ABC-1", Mine: true, State: "In QA", Body: "Thanks! test is ok"}
	if why := rules.Why(e); !strings.Contains(why, "thank-you") {
		t.Fatalf("a sign-off is refused with the reason: %q", why)
	}
	e.Body = "the banner is still wrong on mobile"
	if why := rules.Why(e); why != "" {
		t.Fatalf("a complaint is work: %q", why)
	}
	e.State = "Verified"
	if why := rules.Why(e); !strings.Contains(why, "verified") {
		t.Fatalf("a comment on a verified ticket is not work: %q", why)
	}
}
