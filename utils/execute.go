package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func RunServiceCmd(serviceName string, serviceCommand string, path string) error {
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
			return err
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
