package watch

import (
	"strings"
	"testing"
)

func TestANewIssueSomeoneIsOnIsNotPickedUp(t *testing.T) {
	rules := Rules{Enabled: true}
	for _, state := range []string{"In Progress", "in progress", "Code review", "In Review", "Dev QA", "QA", "Doing", "In Development"} {
		why := rules.Why(Event{Kind: KindIssueNew, Ref: "HUM-1", State: state, Mine: true})
		if !strings.Contains(why, "someone is on it") {
			t.Errorf("state %q: a ticket already in flight is not new work, got %q", state, why)
		}
	}
	for _, state := range []string{"Todo", "Ready", "To Implement", "Backlog", ""} {
		if why := rules.Why(Event{Kind: KindIssueNew, Ref: "HUM-1", State: state, Mine: true}); why != "" {
			t.Errorf("state %q is open work, got %q", state, why)
		}
	}
}

func TestListedStatesTakeInFlightTicketsAnyway(t *testing.T) {
	rules := Rules{Enabled: true, States: []string{"Ready to dev", "In Progress"}}
	if why := rules.Why(Event{Kind: KindIssueNew, Ref: "IMP-1", State: "In Progress", Mine: true}); why != "" {
		t.Fatalf("--states names the column, so it is wanted: %q", why)
	}
}

func TestAnApprovingReviewSummaryIsNotWorkToDo(t *testing.T) {
	rules := Rules{Enabled: true, PRs: true}
	approve := Event{Kind: KindPRComment, Ref: "acme/api#455", Mine: true, Author: "reviewer",
		Body: "## Code review - ABC-1 · api #455\n\n**Verdict: APPROVE** - no blockers in this repo. CI green. All three acceptance criteria met.\n\nSome things I checked and want to credit properly, the flag handling…"}
	if why := rules.Why(approve); !strings.Contains(why, "approves") {
		t.Fatalf("a verdict of approve asks for nothing: %q", why)
	}
	changes := Event{Kind: KindPRComment, Ref: "acme/api#526", Mine: true, Author: "reviewer",
		Body: "## Code review - ABC-1 · api #526\n\n**Verdict: REQUEST_CHANGES - 2 blockers.** CI is green.\n\nThe code is correct - I want to…"}
	if why := rules.Why(changes); why != "" {
		t.Fatalf("request changes is work: %q", why)
	}
	if !IsAcknowledgement("LGTM, approved ✅") {
		t.Fatal("a short sign-off still counts")
	}
}
