package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func RunCmdInPath(serviceCommand string, path string) error {
	fmt.Println("\n🚀 🤖 Executing command: ", art.GreenColor, serviceCommand, art.WhiteColor)

	commandSlice := strings.Fields(serviceCommand)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
