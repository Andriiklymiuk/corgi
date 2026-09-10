package cmd

import (
	"strings"
	"testing"
	"time"
)

// The morning card has to lead with what corgi did on its own, because that
// is the part no notification survived.
func TestWhileAwayLeadsWithWhatCorgiDid(t *testing.T) {
	rep := awayReport{
		Since:   time.Now().Add(-12 * time.Hour),
		Opened:  []awayLine{{Ref: "ABC-1", Workspace: "api", What: "opened 1 PR", URL: "https://x/pull/1"}},
		Failed:  []awayLine{{Ref: "ABC-2", Workspace: "api", What: "timed out"}},
		Waiting: []awayLine{{Ref: "ABC-3", Workspace: "api", What: "still running"}},
		Arrived: []awayLine{{Ref: "ABC-4", Workspace: "api", What: "issue.new"}},
		Deferred: map[string]string{
			"ABC-5": "api",
			"ABC-6": "api",
		},
	}
	out := captureStdout(t, func() { printWhileAway(rep) })

	for _, want := range []string{"corgi opened", "ABC-1", "https://x/pull/1", "corgi could not", "timed out",
		"still running", "arrived", "waiting for a free slot", "ABC-5, ABC-6"} {
		if !strings.Contains(out, want) {
			t.Errorf("the card is missing %q:\n%s", want, out)
		}
	}
	// What corgi did comes before what merely arrived.
	if strings.Index(out, "corgi opened") > strings.Index(out, "arrived") {
		t.Error("what it did on its own leads; what arrived is context")
	}
	// The deferred list is sorted, so two mornings read the same way.
	if strings.Index(out, "ABC-5") > strings.Index(out, "ABC-6") {
		t.Error("deferred refs are sorted")
	}

	quiet := captureStdout(t, func() { printWhileAway(awayReport{Since: time.Now()}) })
	if !strings.Contains(quiet, "a quiet one") {
		t.Errorf("a night with nothing in it says so:\n%s", quiet)
	}
}
