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
		cmd := exec.Command("ps", "ax")
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		if err != nil {
			s.Stop()
			return fmt.Errorf("failed to execute ps command: %v", err)
		}

		vpnAlive := false
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.Contains(line, "AWS") && strings.Contains(line, "isAlive") {
				vpnAlive = true
				break
			}
		}

		if !vpnAlive {
			startCmd := exec.Command("open", "-a", "AWS VPN Client")
			if err := startCmd.Run(); err != nil {
				s.Stop()
				return fmt.Errorf("failed to start AWS VPN Client: %v", err)
			}

			s.Suffix = " Waiting for AWS VPN to start..."
			time.Sleep(5 * time.Second)
		} else {
			s.Stop()
			fmt.Println("\nAWS vpn is opened, so waiting an additional time till you login in your account")
			time.Sleep(10 * time.Second)
			break
		}
	}

	return nil
}
