package utils

import (
	"os"
	"testing"
	"time"
)

func TestIsNotificationsEnabled_Default(t *testing.T) {
	// With no config file (fresh temp HOME), should return false.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	if IsNotificationsEnabled() {
		t.Error("expected notifications disabled by default")
	}
}

func TestIsNotificationsEnabled_True(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	if err := SaveUserConfig(&UserConfig{Notifications: true}); err != nil {
		t.Fatal(err)
	}
	if !IsNotificationsEnabled() {
		t.Error("expected notifications enabled after saving config with Notifications=true")
	}
}

func TestNotify_DisabledNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	ResetNotifyCache()
	t.Cleanup(ResetNotifyCache)

	Notify("test title", "test body")
}

func TestNotifyRaw_DoesNotPanic(t *testing.T) {
	// NotifyRaw fires unconditionally. On CI/test machines the OS tools
	// (osascript/notify-send) may not be present — that is fine, errors are
	// swallowed. Just ensure no panic.
	NotifyRaw("corgi test", "test notification from unit tests")
}

func TestPowershellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "'hello'"},
		{"it's", "'it''s'"},
		{"no quotes", "'no quotes'"},
	}
	for _, tc := range cases {
		got := powershellQuote(tc.in)
		if got != tc.want {
			t.Errorf("powershellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSendNotification_DoesNotPanic(t *testing.T) {
	// Ensure sendNotification doesn't panic on any OS.
	// The underlying command may fail (no osascript in Linux CI) — that's OK.
	_ = os.Getenv("GOOS") // just to make the import used
	sendNotification("corgi", "unit test")
}

func TestClaimNotifyToken_Throttles(t *testing.T) {
	ResetNotifyThrottleForTests()
	t.Cleanup(ResetNotifyThrottleForTests)

	if !claimNotifyToken("a") {
		t.Fatal("first claim should succeed")
	}
	if claimNotifyToken("a") {
		t.Error("second claim within window should be throttled")
	}
	if !claimNotifyToken("b") {
		t.Error("different key should not be throttled")
	}
}

func TestClaimNotifyToken_ExpiresAfterWindow(t *testing.T) {
	ResetNotifyThrottleForTests()
	t.Cleanup(ResetNotifyThrottleForTests)

	orig := notifyThrottleWindow
	notifyThrottleWindow = 10 * time.Millisecond
	t.Cleanup(func() { notifyThrottleWindow = orig })

	if !claimNotifyToken("k") {
		t.Fatal("first claim should succeed")
	}
	time.Sleep(20 * time.Millisecond)
	if !claimNotifyToken("k") {
		t.Error("claim after window should succeed")
	}
}
