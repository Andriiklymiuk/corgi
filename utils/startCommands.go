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

func AwsVpnInit() error {
	if runtime.GOOS == "linux" {
		return fmt.Errorf("this function is not intended to run on Linux")
	}

	s := spinner.New(spinner.CharSets[39], 100*time.Millisecond)
	s.Suffix = " doing woof magic to start aws vpn"

	for {
		s.Start()
		alive, err := isAwsVpnAlive()
		if err != nil {
			s.Stop()
			return err
		}
		if alive {
			s.Stop()
			fmt.Println("\nAWS vpn is opened, so waiting an additional time till you login in your account")
			time.Sleep(10 * time.Second)
			return nil
		}
		if err := launchAwsVpn(s); err != nil {
			return err
		}
	}
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
	time.Sleep(5 * time.Second)
	return nil
}
