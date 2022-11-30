package utils

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func CheckForFlagAndExecuteMake(cmd *cobra.Command, flag string, cmdName string) {
	isFlagPresent, err := cmd.Flags().GetBool(flag)
	if err != nil {
		return
	}

	if !isFlagPresent {
		return
	}
	ExecuteForEachService(cmdName)
}

func ExecuteForEachService(cmdName string) {
	files, err := GetFoldersListInDirectory()
	if err != nil {
		return
	}
	for _, file := range files {
		_, err := ExecuteMakeCommand(file, cmdName)
		if err != nil {
			fmt.Printf("Failed to %s service %s, error: %s", cmdName, file, err)
			return
		}
		fmt.Printf("%s is %s\n", file, cmdName)
	}
}

func CheckForFlagAndExecute(cmd *cobra.Command, flag string, executeFunc func(string) string) {
	shouldStopAllServices, err := cmd.Flags().GetBool(flag)
	if err != nil {
		return
	}

	if !shouldStopAllServices {
		return
	}

	files, err := GetFoldersListInDirectory()
	if err != nil {
		return
	}

	for _, file := range files {
		commandSlice := strings.Fields(executeFunc(file))
		cmd := exec.Command(commandSlice[0], commandSlice[1:]...)
		_, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("%s for %s is successful\n", flag, file)
		}
	}
}
