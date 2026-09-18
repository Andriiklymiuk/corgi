package utils

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func withLinuxVpn(t *testing.T, running func() (bool, error), launch func() error) {
	t.Helper()
	prevRunning, prevLaunch, prevPoll := awsVpnLinuxRunning, awsVpnLinuxLaunch, awsVpnLinuxPoll
	awsVpnLinuxRunning, awsVpnLinuxLaunch, awsVpnLinuxPoll = running, launch, 5*time.Millisecond
	t.Cleanup(func() { awsVpnLinuxRunning, awsVpnLinuxLaunch, awsVpnLinuxPoll = prevRunning, prevLaunch, prevPoll })
}

func TestAwsVpnLinux_RunningClientReturnsAtOnce(t *testing.T) {
	launched := false
	withLinuxVpn(t, func() (bool, error) { return true, nil }, func() error { launched = true; return nil })
	start := time.Now()
	if err := awsVpnInitLinux(); err != nil {
		t.Fatal(err)
	}
	if launched || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("a running client must not be launched again or waited on (launched=%v)", launched)
	}
}

func TestAwsVpnLinux_LaunchesThenWaitsForTheProcess(t *testing.T) {
	withShortPostConnectWait(t)
	polls := 0
	withLinuxVpn(t, func() (bool, error) { polls++; return polls > 2, nil }, func() error { return nil })
	if err := awsVpnInitLinux(); err != nil {
		t.Fatal(err)
	}
	if polls != 3 {
		t.Fatalf("want the client seen on the third poll, got %d polls", polls)
	}
}

func TestAwsVpnLinux_LaunchFailureIsAnError(t *testing.T) {
	withLinuxVpn(t, func() (bool, error) { return false, nil }, func() error { return fmt.Errorf("no desktop session") })
	err := awsVpnInitLinux()
	if err == nil || !strings.Contains(err.Error(), "--omit useAwsVpn") {
		t.Fatalf("want a launch error naming the escape hatch, got %v", err)
	}
}

func TestAwsVpnLinux_StopsAfterBoundedAttempts(t *testing.T) {
	prev := awsVpnMaxLaunchAttempts
	awsVpnMaxLaunchAttempts = 3
	t.Cleanup(func() { awsVpnMaxLaunchAttempts = prev })
	polls := 0
	withLinuxVpn(t, func() (bool, error) { polls++; return false, nil }, func() error { return nil })
	err := awsVpnInitLinux()
	if err == nil || polls != 4 {
		t.Fatalf("want an error after 1 check + 3 polls, got err=%v polls=%d", err, polls)
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
		withLinuxVpn(t, func() (bool, error) { return false, nil }, func() error { return nil })
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
	prev, prevTimeout, prevPoll := awsVpnPostConnectWait, awsVpnConnectTimeout, awsVpnConnectPoll
	awsVpnPostConnectWait, awsVpnConnectTimeout, awsVpnConnectPoll = 10*time.Millisecond, 30*time.Millisecond, time.Millisecond
	t.Cleanup(func() { awsVpnPostConnectWait, awsVpnConnectTimeout, awsVpnConnectPoll = prev, prevTimeout, prevPoll })
}

func TestConnectFirstAwsVpnProfile_WaitsUntilDisconnectButtonAppears(t *testing.T) {
	withShortPostConnectWait(t)
	calls := 0
	withOsascriptRunner(t, func(script string) (string, error) {
		calls++
		if calls == 1 {
			if !strings.Contains(script, "click connectBtn") {
				t.Fatalf("first call must click Connect: %q", script)
			}
			return "connecting\n", nil
		}
		if strings.Contains(script, "click connectBtn") {
			t.Fatal("polling must never click")
		}
		if calls < 4 {
			return "connecting-in-progress\n", nil
		}
		return "already-connected\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("want 1 click + 3 polls, got %d calls", calls)
	}
}

func TestConnectFirstAwsVpnProfile_GivesUpWhenProfileFallsBack(t *testing.T) {
	withShortPostConnectWait(t)
	calls := 0
	withOsascriptRunner(t, func(string) (string, error) {
		calls++
		if calls == 1 {
			return "connecting\n", nil
		}
		return "disconnected\n", nil
	})
	if err := connectFirstAwsVpnProfile(); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestConnectFirstAwsVpnProfile_WaitIsBounded(t *testing.T) {
	withShortPostConnectWait(t)
	withOsascriptRunner(t, func(string) (string, error) { return "connecting\n", nil })
	start := time.Now()
	if err := connectFirstAwsVpnProfile(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("the wait must stop at the timeout")
	}
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
	if !strings.Contains(capturedScript, `is "Connect"`) || !strings.Contains(capturedScript, "entire contents of window 1") {
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
		`"disconnected"`:           true,
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

func TestAwsVpnListed_SeesTheGuiWithoutTheConnectedHelper(t *testing.T) {
	gui := "92743 ?? S 0:12.34 /Applications/AWS VPN Client/AWS VPN Client.app/Contents/MacOS/AWS VPN Client\n"
	helper := "92801 ?? S 0:00.10 /Applications/AWS VPN Client/AWS VPN Client.app/Contents/Resources/openvpn isAlive\n"
	if !awsVpnListed(gui) {
		t.Fatal("a disconnected client is still a running client")
	}
	if !awsVpnListed(helper) {
		t.Fatal("the connected helper still counts")
	}
	if awsVpnListed("1 ?? S 0:00.01 /sbin/launchd\n2 ?? S 0:00.01 /Applications/AWSomeTool.app/Contents/MacOS/x\n") {
		t.Fatal("unrelated processes must not match")
	}
}

func TestAwsVpnLinux_NoPgrepMeansNoBlindLaunch(t *testing.T) {
	launched := false
	withLinuxVpn(t, func() (bool, error) { return false, errNoPgrep }, func() error { launched = true; return nil })
	if err := awsVpnInitLinux(); err != nil || launched {
		t.Fatalf("err=%v launched=%v — without pgrep corgi must neither fail nor launch", err, launched)
	}
}
