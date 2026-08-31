package utils

import (
	"strings"
	"testing"
)

func TestNotifyWithLinkReachesTheSender(t *testing.T) {
	var gotLink string
	previous := sendNotificationOverride
	sendNotificationOverride = func(string, string) { gotLink = notifyLink }
	t.Cleanup(func() { sendNotificationOverride = previous })
	ResetNotifyThrottleForTests()

	NotifyWithLink("corgi agent · idid", "a session is waiting for you", "https://claude.ai/code/s1")
	if gotLink != "https://claude.ai/code/s1" {
		t.Fatalf("the sender saw link %q, want the one passed in", gotLink)
	}
}

func TestPlainNotifyCarriesNoLink(t *testing.T) {
	var gotLink string
	previous := sendNotificationOverride
	sendNotificationOverride = func(string, string) { gotLink = notifyLink }
	t.Cleanup(func() { sendNotificationOverride = previous })
	ResetNotifyThrottleForTests()

	Notify("corgi agent · idid", "plain body with no link")
	if gotLink != "" {
		t.Errorf("a plain Notify must not carry a link, got %q", gotLink)
	}
}

func TestNotifyLinkDoesNotLeakBetweenCalls(t *testing.T) {
	var seen []string
	previous := sendNotificationOverride
	sendNotificationOverride = func(string, string) { seen = append(seen, notifyLink) }
	t.Cleanup(func() { sendNotificationOverride = previous })

	ResetNotifyThrottleForTests()
	NotifyWithLink("t1", "b1", "https://x.test/one")
	ResetNotifyThrottleForTests()
	Notify("t2", "b2")

	if len(seen) != 2 {
		t.Fatalf("expected two sends, got %d", len(seen))
	}
	if !strings.Contains(seen[0], "one") {
		t.Errorf("first send lost its link: %q", seen[0])
	}
	if seen[1] != "" {
		t.Errorf("the link leaked into the next notification: %q", seen[1])
	}
}
