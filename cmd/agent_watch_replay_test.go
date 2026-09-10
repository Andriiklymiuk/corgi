package cmd

import (
	"strings"
	"testing"
)

// The replay's whole job is a tally you can decide from.
func TestReplayTallySaysWhatTheWeekWouldHaveBeen(t *testing.T) {
	list := []replayRow{
		{Ref: "ABC-1", Would: "worked on"},
		{Ref: "ABC-2", Would: "reported"},
		{Ref: "ABC-3", Would: "ignored", Why: "it is done"},
		{Ref: "ABC-4", Would: "worked on"},
	}
	got := replayTally(list)
	for _, want := range []string{"2 worked on", "1 reported", "1 ignored"} {
		if !strings.Contains(got, want) {
			t.Errorf("tally %q is missing %q", got, want)
		}
	}
	if countWould(list, "worked on") != 2 || countWould(list, "nothing") != 0 {
		t.Fatal("the counts have to be the counts")
	}
	if replayTally(nil) != "nothing arrived" {
		t.Fatalf("an empty week says so: %q", replayTally(nil))
	}
	// The order is fixed, so two runs read the same way.
	if i, j := strings.Index(got, "worked on"), strings.Index(got, "ignored"); i > j {
		t.Fatal("what would have been worked on leads")
	}
	if got := sortedKeys(map[string][]replayRow{"web": nil, "api": nil}); got[0] != "api" {
		t.Fatalf("workspaces read alphabetically: %v", got)
	}
}

// --auto-for is meant to be tried before it is saved.
func TestReplayAcceptsASettingToTry(t *testing.T) {
	kinds, err := parseAutoFor("reviews,ci")
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "pr.review" || kinds[1] != "ci.failed" {
		t.Fatalf("kinds = %v", kinds)
	}
	if _, err := parseAutoFor("whatever"); err == nil {
		t.Fatal("a word nobody defined is an error, not a silent empty setting")
	}
}
