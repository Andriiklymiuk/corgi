package utils

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAwsVpnInitLinuxReturnsError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only runs on linux")
	}
	if err := AwsVpnInit(); err == nil {
		t.Error("expected error on linux")
	}
}

func TestConnectFirstAwsVpnProfile_AlreadyConnectedFastPath(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "already-connected\n", nil
	})
	start := time.Now()
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("already-connected must skip wait, took %s", elapsed)
	}
}

func TestConnectFirstAwsVpnProfile_ConnectingInProgress(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "connecting-in-progress\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestAwsVpnInit_AbortsOnShutdownSignal(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("AwsVpnInit returns error early on linux")
	}
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)

	RequestShutdown()

	start := time.Now()
	if err := AwsVpnInit(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("shutdown short-circuit too slow: %s", elapsed)
	}
}

func TestConnectFirstAwsVpnProfile_PostConnectSleepInterruptible(t *testing.T) {
	ResetShutdownForTests()

	prev := awsVpnPostConnectWait
	awsVpnPostConnectWait = 10 * time.Second
	t.Cleanup(func() { awsVpnPostConnectWait = prev })

	withOsascriptRunner(t, func(string) (string, error) {
		return "connecting\n", nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(50 * time.Millisecond)
		RequestShutdown()
	}()
	t.Cleanup(func() {
		<-done
		ResetShutdownForTests()
	})

	start := time.Now()
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("post-connect sleep did not abort on shutdown: %s", elapsed)
	}
}

func TestConnectFirstAwsVpnProfile_AccessibilityDeniedFallback(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "", fmt.Errorf("execution error: Not authorized to send Apple events to System Events. (-1719)")
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("must degrade gracefully on -1719, got err: %v", err)
	}
}

func TestConnectFirstAwsVpnProfile_GenericOsascriptErrFallback(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "", fmt.Errorf("execution error: syntax problem in script (42)")
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("must degrade on generic err too, got err: %v", err)
	}
}

func TestIsAccessibilityDeniedErr(t *testing.T) {
	cases := map[string]bool{
		"": false,
		"execution error: Not authorized to send Apple events to System Events. (-1719)": true,
		"some random failure":                 false,
		"NOT ALLOWED ASSISTIVE ACCESS (1002)": true,
	}
	for in, want := range cases {
		var err error
		if in != "" {
			err = fmt.Errorf("%s", in)
		}
		if got := isAccessibilityDeniedErr(err); got != want {
			t.Errorf("isAccessibilityDeniedErr(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAwsVpnInit_MaxLaunchAttemptsBounded(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("AwsVpnInit returns error early on linux")
	}
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)
	RequestShutdown()

	prev := awsVpnMaxLaunchAttempts
	awsVpnMaxLaunchAttempts = 3
	t.Cleanup(func() { awsVpnMaxLaunchAttempts = prev })

	start := time.Now()
	if err := AwsVpnInit(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("bounded loop took too long: %s", elapsed)
	}
}

func withOsascriptRunner(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := osascriptRunner
	osascriptRunner = fn
	t.Cleanup(func() { osascriptRunner = prev })
}

func withShortPostConnectWait(t *testing.T) {
	t.Helper()
	prev := awsVpnPostConnectWait
	awsVpnPostConnectWait = 10 * time.Millisecond
	t.Cleanup(func() { awsVpnPostConnectWait = prev })
}

func TestConnectFirstAwsVpnProfile_Connecting(t *testing.T) {
	withShortPostConnectWait(t)
	var capturedScript string
	withOsascriptRunner(t, func(script string) (string, error) {
		capturedScript = script
		return "connecting\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(capturedScript, `name is "Connect"`) {
		t.Errorf("script must target Connect button generically, got: %q", capturedScript)
	}
}

func TestConnectFirstAwsVpnProfile_DoesNotLeakProfileName(t *testing.T) {
	var capturedScript string
	withOsascriptRunner(t, func(script string) (string, error) {
		capturedScript = script
		return "connecting\n", nil
	})
	withShortPostConnectWait(t)
	_ = connectFirstAwsVpnProfile()

	allowedQuoted := map[string]bool{
		`"AWS VPN Client"`:         true,
		`"System Events"`:          true,
		`"Disconnect"`:             true,
		`"Connect"`:                true,
		`"Cancel"`:                 true,
		`"no-window"`:              true,
		`"already-connected"`:      true,
		`"connecting"`:             true,
		`"connecting-in-progress"`: true,
		`"no-profile"`:             true,
		`"Remind Me Later"`:        true,
	}
	for _, token := range extractQuoted(capturedScript) {
		if !allowedQuoted[token] {
			t.Errorf("script contains unexpected quoted token %q — possible profile-name leak", token)
		}
	}
}

func extractQuoted(s string) []string {
	var out []string
	inQuote := false
	var buf strings.Builder
	for _, r := range s {
		if r == '"' {
			if inQuote {
				out = append(out, `"`+buf.String()+`"`)
				buf.Reset()
				inQuote = false
			} else {
				inQuote = true
			}
			continue
		}
		if inQuote {
			buf.WriteRune(r)
		}
	}
	return out
}

func TestConnectFirstAwsVpnProfile_AlreadyConnected(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "already-connected\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestConnectFirstAwsVpnProfile_NoProfile(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "no-profile\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestConnectFirstAwsVpnProfile_NoWindow(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "no-window\n", nil
	})
	start := time.Now()
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited too long: %s", elapsed)
	}
}

func TestConnectFirstAwsVpnProfile_OsascriptError(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) {
		return "", fmt.Errorf("Accessibility not authorized")
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatalf("expected graceful degradation, got err: %v", err)
	}
}
