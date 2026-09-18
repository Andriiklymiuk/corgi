package utils

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

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

var awsVpnMaxLaunchAttempts = 6

const awsVpnLinuxBinary = "/opt/awsvpnclient/AWS VPN Client"

var awsVpnLinuxPoll = 5 * time.Second

var errNoPgrep = fmt.Errorf("pgrep is not installed, so corgi cannot tell whether AWS VPN Client is running")

var awsVpnLinuxRunning = func() (bool, error) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		return false, errNoPgrep
	}
	out, err := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", "awsvpnclient").Output()
	return err == nil && strings.TrimSpace(string(out)) != "", nil
}

var awsVpnLinuxLaunch = func() error {
	if _, err := exec.LookPath("gtk-launch"); err == nil {
		if err := exec.Command("gtk-launch", "awsvpnclient").Run(); err == nil {
			return nil
		}
	}
	if _, err := os.Stat(awsVpnLinuxBinary); err != nil {
		return fmt.Errorf("AWS VPN Client is not installed (no awsvpnclient desktop entry, nothing at %s)", awsVpnLinuxBinary)
	}
	cmd := exec.Command(awsVpnLinuxBinary)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func awsVpnInitLinux() error {
	running, err := awsVpnLinuxRunning()
	if err != nil {
		fmt.Printf("ℹ️  %v — open it and connect yourself, or run with --omit useAwsVpn\n", err)
		return nil
	}
	if running {
		fmt.Println("✅ AWS VPN Client is running — connect the profile in its window if it is not up yet")
		return nil
	}
	fmt.Println("🔌 Starting AWS VPN Client — connect the profile in its window")
	if err := awsVpnLinuxLaunch(); err != nil {
		return fmt.Errorf("%w — open it yourself, or run with --omit useAwsVpn", err)
	}
	for attempt := 0; attempt < awsVpnMaxLaunchAttempts; attempt++ {
		if ShutdownRequested() {
			return nil
		}
		InterruptibleSleep(awsVpnLinuxPoll)
		if running, _ := awsVpnLinuxRunning(); running {
			fmt.Printf("   Connect manually within %s...\n", awsVpnPostConnectWait)
			InterruptibleSleep(awsVpnPostConnectWait)
			return nil
		}
	}
	return fmt.Errorf("AWS VPN Client did not come up after %d attempts", awsVpnMaxLaunchAttempts)
}

func AwsVpnInit() error {
	if runtime.GOOS == "linux" {
		return awsVpnInitLinux()
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

var awsVpnConnectTimeout = 90 * time.Second

var awsVpnConnectPoll = 3 * time.Second

const awsVpnScanScript = `
tell application "System Events"
	tell process "AWS VPN Client"
		-- dismiss any update / nag dialog that may cover the main window
		repeat 3 times
			if (exists window 1) and (exists (first button of window 1 whose name is "Remind Me Later")) then
				click (first button of window 1 whose name is "Remind Me Later")
				delay 0.3
			else
				exit repeat
			end if
		end repeat
		if not (exists window 1) then
			tell application "AWS VPN Client" to activate
			delay 1
		end if
		if not (exists window 1) then return "no-window"
		-- client 5.x lists profiles; each row carries its own Connect / Disconnect button, nested in groups
		set hasDisconnect to false
		set hasCancel to false
		set connectBtn to missing value
		set els to entire contents of window 1
		repeat with e in els
			try
				if (class of e) is button then
					set n to (name of e) as text
					if n is "Disconnect" then set hasDisconnect to true
					if n is "Cancel" then set hasCancel to true
					if n is "Connect" and connectBtn is missing value then set connectBtn to e
				end if
			end try
		end repeat
		if hasDisconnect then return "already-connected"
		if hasCancel then return "connecting-in-progress"
		if connectBtn is not missing value then
			__ON_CONNECT__
		end if
		return "no-profile"
	end tell
end tell
`

const awsVpnClickConnect = `tell application "AWS VPN Client" to activate
			delay 0.3
			click connectBtn
			return "connecting"`

const awsVpnReadOnly = `return "disconnected"`

func awsVpnScript(onConnect string) string {
	return strings.Replace(awsVpnScanScript, "__ON_CONNECT__", onConnect, 1)
}

func connectFirstAwsVpnProfile() error {
	out, err := osascriptRunner(awsVpnScript(awsVpnClickConnect))
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
		awsVpnWaitConnected()
	case "connecting":
		fmt.Println("🔌 Connecting first AWS VPN profile...")
		awsVpnWaitConnected()
	case "no-profile":
		fmt.Println("⚠️  AWS VPN Client: no Connect button found in main window.")
		fmt.Println("   Possible causes: profile missing, modal/sheet still open, or window not on front display.")
		fmt.Println("   Open AWS VPN Client, dismiss any update dialog, then re-run corgi.")
	case "no-window":
		fmt.Println("⚠️  AWS VPN Client window not ready. Connect manually.")
		InterruptibleSleep(awsVpnPostConnectWait)
	}
	return nil
}

func awsVpnWaitConnected() {
	fmt.Println("   Finish the sign-in in your browser; Safari may warn that the form is sent insecurely — that is the client's own callback on 127.0.0.1.")
	deadline := time.Now().Add(awsVpnConnectTimeout)
	for time.Now().Before(deadline) && !ShutdownRequested() {
		InterruptibleSleep(awsVpnConnectPoll)
		out, err := osascriptRunner(awsVpnScript(awsVpnReadOnly))
		if err != nil {
			InterruptibleSleep(awsVpnPostConnectWait)
			return
		}
		switch strings.TrimSpace(out) {
		case "already-connected":
			fmt.Println("✅ AWS VPN connected")
			return
		case "disconnected", "no-profile":
			fmt.Println("⚠️  AWS VPN connection did not complete — the profile is back to Disconnected. Connect it manually.")
			return
		}
	}
	if !ShutdownRequested() {
		fmt.Printf("⚠️  AWS VPN still connecting after %s — going on without waiting; check the client window.\n", awsVpnConnectTimeout)
	}
}

func isAwsVpnAlive() (bool, error) {
	cmd := exec.Command("ps", "ax")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to execute ps command: %v", err)
	}
	return awsVpnListed(out.String()), nil
}

func awsVpnListed(ps string) bool {
	for _, line := range strings.Split(ps, "\n") {
		if strings.Contains(line, "AWS VPN Client.app/Contents/MacOS/") || (strings.Contains(line, "AWS") && strings.Contains(line, "isAlive")) {
			return true
		}
	}
	return false
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
