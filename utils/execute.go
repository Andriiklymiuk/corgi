package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func RunServiceCmd(serviceName string, serviceCommand string, path string) error {
	fmt.Println(serviceCommand)
	lines := strings.Split(serviceCommand, "\n")
	var accumulatedCommand string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// If the line ends with a backslash, remove it and append the line to the next
		if strings.HasSuffix(line, "\\") {
			accumulatedCommand += strings.TrimSuffix(line, "\\") + " "
			continue
		}

		// Execute the accumulated command if any, otherwise execute the line itself
		finalCommand := line
		if accumulatedCommand != "" {
			finalCommand = accumulatedCommand + line
			accumulatedCommand = ""
		}
		executingMessage := fmt.Sprintf("\n🚀 🤖 Executing command for %s: ", serviceName)
		fmt.Println(executingMessage, art.GreenColor, finalCommand, art.WhiteColor)

		commandSlice := strings.Fields(finalCommand)
		if len(commandSlice) == 0 {
			continue
		}

		cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

		cmd.Dir = path
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			// Check the error directly
			if strings.Contains(err.Error(), "executable file not found") {
				// Attempt to install missing command
				missingCommand := commandSlice[0]
				if cmdInfo, ok := CommandInstructions[missingCommand]; ok {
					fmt.Printf("\n❗%s is missing. Attempting to install it using: %s\n", missingCommand, cmdInfo.Install)
					installCmd := exec.Command("/bin/bash", "-c", cmdInfo.Install)
					installCmd.Dir = path
					installCmd.Stdout = os.Stdout
					installCmd.Stderr = os.Stderr
					if err := installCmd.Run(); err != nil {
						return fmt.Errorf("failed to install %s: %v", missingCommand, err)
					}
					// Rerun the original command
					fmt.Printf("\n🔄 Retrying the command: %s\n", finalCommand)
					return RunServiceCmd(serviceName, finalCommand, path)
				} else {
					return fmt.Errorf("unknown command %s, no install instructions found", missingCommand)
				}
			} else {
				return err
			}
		}
	}
	return nil
}

func RunCombinedCmd(command string, path string) error {
	fmt.Println("🚀 🤖 Executing command: ", art.GreenColor, command, art.WhiteColor)

	commandSlice := strings.Fields(command)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)
	_, err := cmd.CombinedOutput()
	return err
}
