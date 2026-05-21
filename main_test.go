package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestShouldPromptToContinueSkippedNonInteractive(t *testing.T) {
	if shouldPromptToContinue(true) {
		t.Errorf("must not prompt to continue when non-interactive")
	}
	if !shouldPromptToContinue(false) {
		t.Errorf("interactive mode should allow the prompt")
	}
}

func withArgs(t *testing.T, args ...string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

func TestCanRunCliAgain(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"corgi", "db"}, true},
		{[]string{"corgi", "create"}, true},
		{[]string{"corgi", "fork"}, true},
		{[]string{"corgi", "status"}, false},
		{[]string{"corgi", "-v"}, false}, // a flag short-circuits to false
		{[]string{"corgi"}, false},
	}
	for _, tc := range cases {
		withArgs(t, tc.args...)
		if got := canRunCliAgain(); got != tc.want {
			t.Errorf("args=%v: got %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestCanShowWelcomeMessages(t *testing.T) {
	origCI := utils.CIMode
	defer func() { utils.CIMode = origCI }()

	utils.CIMode = false
	withArgs(t, "corgi", "run")
	if !canShowWelcomeMessages() {
		t.Error("expected welcome for plain run command")
	}

	for _, suppress := range []string{"status", "doctor", "--json", "--version", "completion"} {
		withArgs(t, "corgi", suppress)
		if canShowWelcomeMessages() {
			t.Errorf("arg %q should suppress welcome", suppress)
		}
	}

	utils.CIMode = true
	withArgs(t, "corgi", "run")
	if canShowWelcomeMessages() {
		t.Error("CI mode should suppress welcome")
	}
}

func TestShowWelcomeMessage_SuppressedIsSilent(t *testing.T) {
	withArgs(t, "corgi", "status")
	if out := captureStdout(t, showWelcomeMessage); out != "" {
		t.Errorf("expected no output when suppressed, got %q", out)
	}
}

func TestShowFinalMessage_SuppressedIsSilent(t *testing.T) {
	withArgs(t, "corgi", "status")
	if out := captureStdout(t, showFinalMessage); out != "" {
		t.Errorf("expected no output when suppressed, got %q", out)
	}
}

func TestClearTerminal_EarlyReturnOnSuppressingArg(t *testing.T) {
	// A suppressing arg must make ClearTerminal a no-op (no terminal reset exec).
	withArgs(t, "corgi", "status")
	ClearTerminal() // must not panic or run a clear command
}

func TestRunClearCmd_HarmlessCommand(t *testing.T) {
	// Exercises the exec path with a no-op binary available on POSIX systems.
	if _, err := os.Stat("/usr/bin/true"); err == nil {
		runClearCmd("/usr/bin/true")
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(buf.String())
}
