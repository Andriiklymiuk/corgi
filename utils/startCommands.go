package utils

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

// osascriptRunner is injected by tests. Wraps *ExitError so caller can
// match stderr (e.g. -1719 for Accessibility denied).
var osascriptRunner = func(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return string(out), err
}

func isAccessibilityDeniedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "-1719") ||
		strings.Contains(msg, "not authorized to send apple events") ||
		strings.Contains(msg, "not allowed assistive access")
}

var awsVpnPostConnectWait = 8 * time.Second

// awsVpnMaxLaunchAttempts bounds the launch retry loop; ~30s budget.
var awsVpnMaxLaunchAttempts = 6

// AwsVpnInit launches AWS VPN Client and auto-connects the first profile
// when Accessibility permission is granted; otherwise falls back to a
// manual-click wait.
func AwsVpnInit() error {
	if runtime.GOOS == "linux" {
		return fmt.Errorf("this function is not intended to run on Linux")
	}

	s := spinner.New(spinner.CharSets[39], 100*time.Millisecond)
	s.Suffix = " doing woof magic to start aws vpn"

	for attempt := 0; attempt < awsVpnMaxLaunchAttempts; attempt++ {
		if ShutdownRequested() {
			s.Stop()
			return nil
		}
		s.Start()
		alive, err := isAwsVpnAlive()
		if err != nil {
			s.Stop()
			return err
		}
		if alive {
			s.Stop()
			return connectFirstAwsVpnProfile()
		}
		if err := launchAwsVpn(s); err != nil {
			return err
		}
	}
	s.Stop()
	return fmt.Errorf("AWS VPN Client failed to become ready after %d attempts", awsVpnMaxLaunchAttempts)
}

// connectFirstAwsVpnProfile clicks the first profile's Connect button.
// AWS VPN Client has no CLI — GUI automation is the only path. State
// detection runs without activating to avoid focus steal; activates only
// when clicking Connect.
//
// Script uses only generic UI labels (Connect/Disconnect/Cancel) — no
// profile names. See TestConnectFirstAwsVpnProfile_DoesNotLeakProfileName.
func connectFirstAwsVpnProfile() error {
	const script = `
tell application "System Events"
	tell process "AWS VPN Client"
		if not (exists window 1) then return "no-window"
		if exists (first button of window 1 whose name is "Disconnect") then return "already-connected"
		if exists (first button of window 1 whose name is "Cancel") then return "connecting-in-progress"
		if exists (first button of window 1 whose name is "Connect") then
			tell application "AWS VPN Client" to activate
			delay 0.3
			click (first button of window 1 whose name is "Connect")
			return "connecting"
		end if
		return "no-profile"
	end tell
end tell
`
	out, err := osascriptRunner(script)
	if err != nil {
		if isAccessibilityDeniedErr(err) {
			fmt.Println("ℹ️  AWS VPN auto-connect skipped (Accessibility permission not granted).")
			fmt.Println("   To enable: System Settings → Privacy & Security → Accessibility → add your terminal app.")
		} else {
			fmt.Println("⚠️  AWS VPN auto-connect failed:", err)
		}
		fmt.Printf("   Connect manually within %s...\n", awsVpnPostConnectWait)
		InterruptibleSleep(awsVpnPostConnectWait)
		return nil
	}
	switch strings.TrimSpace(out) {
	case "already-connected":
		fmt.Println("✅ AWS VPN already connected, skipping")
	case "connecting-in-progress":
		fmt.Println("⏳ AWS VPN handshake already in progress, waiting...")
		InterruptibleSleep(awsVpnPostConnectWait)
	case "connecting":
		fmt.Println("🔌 Connecting first AWS VPN profile...")
		InterruptibleSleep(awsVpnPostConnectWait)
	case "no-profile":
		fmt.Println("⚠️  No AWS VPN profile available in client. Add one and re-run.")
	case "no-window":
		fmt.Println("⚠️  AWS VPN Client window not ready. Connect manually.")
		InterruptibleSleep(awsVpnPostConnectWait)
	}
	return nil
}

func isAwsVpnAlive() (bool, error) {
	cmd := exec.Command("ps", "ax")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to execute ps command: %v", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "AWS") && strings.Contains(line, "isAlive") {
			return true, nil
		}
	}
	return false, nil
}

func launchAwsVpn(s *spinner.Spinner) error {
	startCmd := exec.Command("open", "-a", "AWS VPN Client")
	if err := startCmd.Run(); err != nil {
		s.Stop()
		return fmt.Errorf("failed to start AWS VPN Client: %v", err)
	}
	s.Suffix = " Waiting for AWS VPN to start..."
	InterruptibleSleep(5 * time.Second)
	return nil
}
