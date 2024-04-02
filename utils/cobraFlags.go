package utils

import (
	"fmt"
	"os"

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

	isRunOnce, err := cmd.Root().Flags().GetBool("runOnce")
	if err != nil {
		return
	}
	if isRunOnce {
		PrintFinalMessage()
		os.Exit(0)
	}
}

func ExecuteForEachService(cmdName string) {
	files, err := GetFoldersListInDirectory()
	if err != nil {
		return
	}
	for _, file := range files {
		err := ExecuteCommandRun(file, "make", cmdName)
		if err != nil {
			fmt.Printf("Failed to %s service %s, error: %s", cmdName, file, err)
			return
		}
		fmt.Printf("%s is %s\n", file, cmdName)
	}
}
