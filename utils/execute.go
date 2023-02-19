package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func RunServiceCmd(serviceName string, serviceCommand string, path string) error {
	executingMessage := fmt.Sprintf("\n🚀 🤖 Executing command for %s: ", serviceName)
	fmt.Println(executingMessage, art.GreenColor, serviceCommand, art.WhiteColor)

	commandSlice := strings.Fields(serviceCommand)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func RunCombinedCmd(command string, path string) error {
	fmt.Println("🚀 🤖 Executing command: ", art.GreenColor, command, art.WhiteColor)

	commandSlice := strings.Fields(command)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)
	_, err := cmd.CombinedOutput()
	return err
}
