package watch

import (
	"strings"
	"testing"
	"time"
)

func TestScenariosLiveOutsideTheRepoAndComeBackInOrder(t *testing.T) {
	dir := t.TempDir()
	if err := AddScenario(dir, "acme", "refund", "A customer asks for a refund and sees it."); err != nil {
		t.Fatal(err)
	}
	if err := AddScenario(dir, "acme", "checkout", "A customer pays with a coupon."); err != nil {
		t.Fatal(err)
	}
	got := LoadScenarios(dir, "acme")
	if len(got) != 2 || got[0].Name != "checkout" || got[1].Name != "refund" {
		t.Fatalf("got %+v", got)
	}
	if err := RemoveScenario(dir, "acme", "checkout"); err != nil {
		t.Fatal(err)
	}
	if got := LoadScenarios(dir, "acme"); len(got) != 1 {
		t.Fatalf("after remove: %+v", got)
	}
	if got := LoadScenarios(dir, "other"); len(got) != 0 {
		t.Fatalf("another workspace sees them: %+v", got)
	}
}

func TestAScenarioNameCannotLeaveItsFolder(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../evil", "a/b", "UP", ""} {
		if err := AddScenario(dir, "acme", bad, "text"); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	if err := AddScenario(dir, "acme", "fine", "  "); err == nil {
		t.Fatal("an empty scenario was accepted")
	}
}

func TestAJudgeThatGivesNoVerdictIsNotAPass(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"I checked it.\n{\"satisfied\":true,\"why\":\"works\"}", true},
		{"```\n{\"satisfied\":false,\"why\":\"no refund\"}\n```", false},
		{"Looks great to me!", false},
		{"{\"why\":\"forgot the field\"}", false},
		{"{\"satisfied\":true}\nthen more words", true},
	}
	for _, c := range cases {
		if got, _ := ParseVerdict(c.text); got != c.want {
			t.Fatalf("%q: got %v", c.text, got)
		}
	}
	if _, why := ParseVerdict("nothing"); !strings.Contains(why, "no verdict") {
		t.Fatalf("why: %q", why)
	}
}

func TestSatisfactionIsTheShareOfRunsThatPassed(t *testing.T) {
	now := time.Now()
	r := []ScenarioResult{{Satisfied: true, At: now}, {Satisfied: true, At: now}, {Satisfied: false, At: now}, {Satisfied: true, At: now}}
	if got := Satisfaction(r); got != 75 {
		t.Fatalf("got %d", got)
	}
	if got := Satisfaction(nil); got != 0 {
		t.Fatalf("no runs: %d", got)
	}
}
