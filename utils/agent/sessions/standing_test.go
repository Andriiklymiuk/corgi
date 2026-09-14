package sessions

import "testing"

func TestStandingLadder(t *testing.T) {
	open := &PullFacts{State: "open", Checks: "passing", Review: "approved"}
	cases := []struct {
		name string
		f    Facts
		word string
		why  string
	}{
		{"nothing known", Facts{}, StandNew, ""},
		{"merged beats everything", Facts{Pull: &PullFacts{State: "merged"}, Pending: &Pending{Tool: "Bash"}}, StandMerged, ""},
		{"closed", Facts{Pull: &PullFacts{State: "closed"}, Status: StatusWorking}, StandClosed, ""},
		{"blocked wall", Facts{Blocked: "runs failed twice", Pull: open}, StandBlocked, "runs failed twice"},
		{"permission prompt", Facts{Status: StatusNeedsInput, Pending: &Pending{Tool: "Bash", Subject: "rm -rf build"}, Pull: open}, StandNeedsYou, "allow Bash rm -rf build?"},
		{"waiting for a word", Facts{Status: StatusNeedsInput}, StandNeedsYou, "waiting for a word"},
		{"gate red three times", Facts{Status: StatusWorking, Gate: &GateRun{OK: false, Fails: 3, Cmd: "go test ./..."}}, StandNeedsYou, "gate red 3× · go test ./..."},
		{"at a limit", Facts{Status: StatusLimited, Limit: LimitQuota, Pull: &PullFacts{State: "open", Checks: "failing"}}, StandLimited, "quota"},
		{"checks failing", Facts{Status: StatusWorking, Pull: &PullFacts{State: "open", Checks: "failing", Review: "approved"}}, StandChecksRed, "checks ✗ · approved"},
		{"changes requested", Facts{Pull: &PullFacts{State: "open", Checks: "passing", Review: "changes"}}, StandChanges, "checks ✓ · changes requested"},
		{"conflicts with main", Facts{Status: StatusWorking, Behind: &Behind{Commits: 4, Conflicts: []string{"api/x.go", "y.go"}}, Pull: open}, StandConflicts, "main moved 4 · conflicts in x.go, y.go"},
		{"tests red", Facts{Status: StatusWorking, Tests: &TestRun{OK: false, Cmd: "go test ./..."}, Pull: open}, StandTestsRed, "tests ✗ go test ./..."},
		{"gate red once", Facts{Status: StatusDone, Gate: &GateRun{OK: false, Fails: 1, Cmd: "make lint"}}, StandTestsRed, "gate ✗ make lint"},
		{"ready to merge", Facts{Status: StatusDone, Pull: open}, StandReady, "checks ✓ · approved"},
		{"ready with no checks", Facts{Pull: &PullFacts{State: "open", Checks: "none", Review: "approved"}}, StandReady, "approved"},
		{"approved, checks running", Facts{Pull: &PullFacts{State: "open", Checks: "pending", Review: "approved"}}, StandApproved, "checks running · approved"},
		{"draft", Facts{Pull: &PullFacts{State: "draft"}, Status: StatusWorking}, StandDraft, ""},
		{"in review", Facts{Pull: &PullFacts{State: "open", Checks: "passing", Review: "pending"}}, StandReview, "checks ✓ · review pending"},
		{"open, forge not asked yet", Facts{PR: "https://github.com/acme/api/pull/7", Status: StatusDone}, StandPROpen, ""},
		{"pull beats a working session", Facts{PR: "https://github.com/acme/api/pull/7", Status: StatusWorking}, StandPROpen, ""},
		{"stuck", Facts{Status: StatusWorking, Stuck: true}, StandStuck, "working, silent 12 min"},
		{"working", Facts{Status: StatusWorking}, StandWorking, ""},
		{"done", Facts{Status: StatusDone}, StandDone, ""},
		{"idle", Facts{Status: StatusStale}, StandIdle, ""},
		{"gone", Facts{Status: StatusGone}, StandGone, ""},
		{"handoff waits", Facts{Handoff: "api: cart fix"}, StandHandoff, "api: cart fix"},
	}
	for _, c := range cases {
		got := StandingOf(c.f)
		if got.Word != c.word || got.Why != c.why {
			t.Errorf("%s: got %q / %q, want %q / %q", c.name, got.Word, got.Why, c.word, c.why)
		}
	}
}

func TestStandingLine(t *testing.T) {
	if l := (Standing{Word: StandReady, Why: "checks ✓ · approved"}).Line(); l != "ready to merge · checks ✓ · approved" {
		t.Fatalf("line: %q", l)
	}
	if l := (Standing{Word: StandWorking}).Line(); l != "working" {
		t.Fatalf("line: %q", l)
	}
	if l := (Standing{}).Line(); l != "" {
		t.Fatalf("empty: %q", l)
	}
}

func TestPullWords(t *testing.T) {
	if !PullReady(PullFacts{State: "open", Checks: "passing", Review: "approved"}) {
		t.Fatal("green + approved is ready")
	}
	if PullReady(PullFacts{State: "draft", Checks: "passing", Review: "approved"}) {
		t.Fatal("a draft is never ready")
	}
	if l := PullLine(PullFacts{State: "open", Checks: "failing", Review: "changes"}); l != "checks ✗ · changes requested" {
		t.Fatalf("line: %q", l)
	}
	if l := PullLine(PullFacts{State: "open", Checks: "passing", Review: "approved"}); l != "ready to merge · checks ✓ · approved" {
		t.Fatalf("ready line: %q", l)
	}
}

func TestSessionFacts(t *testing.T) {
	s := Session{Status: StatusWorking, Stuck: true, PR: "https://github.com/acme/api/pull/7",
		Tests: &TestRun{OK: false, Cmd: "go test"}, Behind: &Behind{Commits: 1}}
	f := s.Facts(nil)
	if f.Status != StatusWorking || !f.Stuck || f.PR != s.PR || f.Tests != s.Tests || f.Behind != s.Behind || f.Pull != nil {
		t.Fatalf("facts: %+v", f)
	}
	pull := &PullFacts{State: "open", Checks: "failing"}
	if got := StandingOf(s.Facts(pull)); got.Word != StandChecksRed {
		t.Fatalf("with the forge's word: %q", got.Word)
	}
}

// The board carries every session's standing, and a key its word: the
// forge's word on the pull request when the daemon has one, the link alone
// until then, the session itself when there is no pull request.
func TestTheBoardCarriesEverySessionsStanding(t *testing.T) {
	r := newTestRegistry(t)
	e := ev("UserPromptSubmit", "s1", 0)
	e.PR = "https://github.com/acme/api/pull/7"
	r.Apply(e)
	other := ev("UserPromptSubmit", "s2", 0)
	other.ClaudePID, other.Ancestors = 200, []int{200}
	r.Apply(other)
	st := r.Snapshot(t0)
	words := map[string]string{}
	for _, s := range st.Sessions {
		words[s.ID] = s.Standing.Word
	}
	if words["s1"] != StandPROpen || words["s2"] != StandWorking {
		t.Fatalf("link alone, session alone: %v", words)
	}
	r.PullFor = func(link string) (PullFacts, bool) {
		if link != e.PR {
			return PullFacts{}, false
		}
		return PullFacts{State: "open", Checks: "passing", Review: "approved"}, true
	}
	st = r.Snapshot(t0)
	var s1 Session
	for _, s := range st.Sessions {
		if s.ID == "s1" {
			s1 = s
		}
	}
	if s1.Standing == nil || s1.Standing.Word != StandReady || s1.Standing.Why != "checks ✓ · approved" {
		t.Fatalf("the forge's word: %+v", s1.Standing)
	}
	found := false
	for _, sl := range st.Slots {
		if sl.SessionID == "s1" {
			found = sl.Standing == StandReady
		}
	}
	if !found {
		t.Fatal("a key gets the word")
	}
}
