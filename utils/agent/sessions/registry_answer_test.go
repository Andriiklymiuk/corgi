package sessions

import (
	"testing"
	"time"
)

func waitingOn(t *testing.T, subject string) *Registry {
	t.Helper()
	r := newTestRegistry(t)
	r.Apply(ev("SessionStart", "s1", 0))
	perm := ev("PermissionRequest", "s1", time.Second)
	perm.Tool, perm.Subject = "Bash", subject
	r.Apply(perm)
	return r
}

func TestADeniedPromptStopsAsking(t *testing.T) {
	r := waitingOn(t, "go test ./...")
	if !r.Denied("s1", time.Now()) {
		t.Fatal("a waiting prompt answered with Esc must settle")
	}
	s, _ := r.Lookup("s1")
	if s.Status != StatusDone || s.Pending != nil {
		t.Fatalf("Esc ends the turn with no hook; the row must stop asking: %+v", s)
	}
	if r.Denied("s1", time.Now()) {
		t.Fatal("nothing left to deny")
	}
}

func TestARiskyAllowNeedsTheConfirmedAnswer(t *testing.T) {
	r := waitingOn(t, "rm -rf build")
	if _, err := r.PendingAnswer("s1", "allow"); err == nil {
		t.Fatal("an unconfirmed allow of rm must be refused")
	}
	if keys, err := r.PendingAnswerConfirmed("s1", "allow"); err != nil || keys != "\r" {
		t.Fatalf("a confirmed allow presses enter: %q %v", keys, err)
	}
	if _, err := r.PendingAnswerConfirmed("s1", "always"); err == nil {
		t.Fatal("always for a risky command stays a laptop decision")
	}
}

func TestOnlyATypableHostIsAnswerable(t *testing.T) {
	for _, h := range []Host{{Kind: HostITerm}, {Kind: HostTerminalApp}, {Kind: HostTmux}, {Kind: HostVSCodeTerminal, WindowID: "w1", Connected: true}} {
		if why := h.Answerable(); why != "" {
			t.Errorf("%+v: %s", h, why)
		}
	}
	for _, h := range []Host{{Kind: HostVSCodePanel}, {Kind: HostVSCodeTerminal}, {Kind: HostUnknown}, {}} {
		if h.Answerable() == "" {
			t.Errorf("%+v cannot take keys; it must say so", h)
		}
	}
}
