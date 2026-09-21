package utils

import (
	"strings"
	"testing"
)

func TestTerminalNotifierArgsRunTheCommandOnClick(t *testing.T) {
	args := terminalNotifierArgs("corgi agent · api", "drifting", "/icon.png", "https://x.test/app", []string{"/usr/local/bin/corgi", "agent", "focus", "ab cd"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-execute /usr/local/bin/corgi agent focus 'ab cd'") {
		t.Fatalf("click must run the focus command, shell-quoted: %v", args)
	}
	if strings.Contains(joined, "-open") || strings.Contains(joined, "-sender") {
		t.Fatalf("a command wins over the link, and -sender (removed upstream) must not appear: %v", args)
	}
}

func TestTerminalNotifierArgsOpenTheLinkWithoutACommand(t *testing.T) {
	args := terminalNotifierArgs("t", "b", "", "https://x.test/app", nil)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-open https://x.test/app") || strings.Contains(joined, "-execute") {
		t.Fatalf("no command: the link opens on click: %v", args)
	}
}

func TestTerminalNotifierArgsPlainHasNoClickAction(t *testing.T) {
	args := terminalNotifierArgs("t", "b", "", "", nil)
	joined := strings.Join(args, " ")
	for _, flag := range []string{"-open", "-execute", "-sender", "-activate"} {
		if strings.Contains(joined, flag) {
			t.Fatalf("a plain notification carries no click action, got %s in %v", flag, args)
		}
	}
}

func TestShellQuoteWrapsUnsafeWords(t *testing.T) {
	cases := map[string]string{
		"corgi":       "corgi",
		"a b":         "'a b'",
		"it's":        "'it'\\''s'",
		"/usr/bin/x":  "/usr/bin/x",
		"$(rm -rf /)": "'$(rm -rf /)'",
		"":            "''",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotifyWithCommandClearsAfterwards(t *testing.T) {
	SilenceNotificationsForTests()
	t.Cleanup(func() { sendNotificationOverride = nil })
	enableNotificationsForTest(t)
	ResetNotifyThrottleForTests()

	NotifyWithCommand("t", "b", []string{"corgi", "agent", "focus", "one"}, "https://x.test/app")
	if len(notifyCommand) != 0 || notifyLink != "" {
		t.Fatalf("the command and link must not leak into the next notification: %v %q", notifyCommand, notifyLink)
	}
}
