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
