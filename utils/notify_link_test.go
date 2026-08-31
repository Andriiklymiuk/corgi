package utils

import "testing"

func TestSafeNotifyLinkOnlyAllowsWebURLs(t *testing.T) {
	ok := map[string]string{
		"https://claude.ai/code?environment=env_1": "https://claude.ai/code?environment=env_1",
		"http://192.168.1.4:8765/app":              "http://192.168.1.4:8765/app",
		"  https://x.test/app  ":                   "https://x.test/app",
	}
	for in, want := range ok {
		if got := safeNotifyLink(in); got != want {
			t.Errorf("safeNotifyLink(%q) = %q, want %q", in, got, want)
		}
	}

	for _, bad := range []string{
		"", "   ", "file:///Users/me/secret", "/tmp/thing",
		"x-apple-shortcut://run", "javascript:alert(1)", "https://",
	} {
		if got := safeNotifyLink(bad); got != "" {
			t.Errorf("safeNotifyLink(%q) = %q, want it refused", bad, got)
		}
	}
}

func TestNotifyWithLinkClearsTheLinkAfterwards(t *testing.T) {
	SilenceNotificationsForTests()
	t.Cleanup(func() { sendNotificationOverride = nil })

	NotifyWithLink("t", "b", "https://x.test/app")
	if notifyLink != "" {
		t.Errorf("the link must not leak into the next notification, got %q", notifyLink)
	}
}
