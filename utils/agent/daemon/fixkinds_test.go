package daemon

import (
	"testing"
	"time"

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

// The dry run is the tool people use to decide whether to trust unattended
// mode, and it reported "fix" for kinds --auto-for never named: the check
// read p.Matched before it was assigned.
func TestTheDryRunSaysNotifyForAKindThatIsNotWorkedOn(t *testing.T) {
	d := &Daemon{Dir: t.TempDir(), Watches: []WatchSpec{{
		Workspace: "api", Project: "ABC", ConfigDir: t.TempDir(),
		Rules:    watch.Rules{Enabled: true, Comments: true, PRs: true, Reviews: true, CI: true},
		Action:   "fix",
		FixKinds: []string{"pr.review"},
	}}}
	d.watchState = watch.LoadState(d.Dir)

	worked, ok := d.ProbeEvent(watch.Event{Kind: watch.KindPRReview, Ref: "acme/api#7", Mine: true}, time.Now())
	if !ok || !worked.Matched || worked.Action != "fix" {
		t.Fatalf("a named kind is worked on: %+v", worked)
	}
	for _, kind := range []watch.Kind{watch.KindIssueComment, watch.KindPRComment, watch.KindCIFailed} {
		p, ok := d.ProbeEvent(watch.Event{Kind: kind, Ref: "acme/api#7", Mine: true}, time.Now())
		if !ok || !p.Matched {
			t.Fatalf("%s still matches the rules: %+v", kind, p)
		}
		if p.Action != "notify" {
			t.Errorf("%s was not named in --auto-for, so the dry run must say notify, got %q", kind, p.Action)
		}
	}
	// A reporting workspace is unaffected either way.
	quiet := &Daemon{Dir: t.TempDir(), Watches: []WatchSpec{{
		Workspace: "api", Project: "ABC", Rules: watch.Rules{Enabled: true, PRs: true}, Action: "notify",
	}}}
	quiet.watchState = watch.LoadState(quiet.Dir)
	if p, _ := quiet.ProbeEvent(watch.Event{Kind: watch.KindPRReview, Ref: "acme/api#7", Mine: true}, time.Now()); p.Action != "notify" {
		t.Fatalf("a reporting watch reports: %+v", p)
	}
}
