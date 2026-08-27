package supervisor

import (
	"testing"
)

func TestSessionLinkScannerCapturesEachDistinctSession(t *testing.T) {
	var got []string
	s := newSessionLinkScanner(func(id string) { got = append(got, id) })

	// Real remote-control output: OSC-8 hyperlink escapes around the URL, a
	// query string, and the same session printed twice.
	chunk := "\x1b]8;;https://claude.ai/code/session_01AAA?from=cliAttached\x1b]8;;\x1b\\\n" +
		"link https://claude.ai/code/session_01AAA?from=cliprobe\n" +
		"and https://claude.ai/code/session_02BBB done\n"
	_, _ = s.Write([]byte(chunk))

	if len(got) != 2 || got[0] != "session_01AAA" || got[1] != "session_02BBB" {
		t.Fatalf("scanner must report each distinct session once, got %v", got)
	}

	// An id split across writes must not be reported truncated.
	_, _ = s.Write([]byte("see https://claude.ai/code/session_03C"))
	_, _ = s.Write([]byte("CC now\n"))
	if len(got) != 3 || got[2] != "session_03CCC" {
		t.Fatalf("a split id must be assembled whole, got %v", got)
	}
}

func TestRunnerAddSessionLinkDedupsAndCaps(t *testing.T) {
	r := NewRunner(baseConfig(), nil, NewWakeLock(WakeLockOff))
	for i := 0; i < 2; i++ {
		r.addSessionLink("session_01X")
	}
	if n := len(r.State().Sessions); n != 1 {
		t.Fatalf("the same session must be recorded once, got %d", n)
	}
	if r.State().Sessions[0] != "https://claude.ai/code/session_01X" {
		t.Fatalf("stored value must be the canonical URL, got %q", r.State().Sessions[0])
	}
	for i := 0; i < maxTrackedSessions+5; i++ {
		r.addSessionLink("session_" + string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	if n := len(r.State().Sessions); n > maxTrackedSessions {
		t.Fatalf("sessions must cap at %d, got %d", maxTrackedSessions, n)
	}
}
