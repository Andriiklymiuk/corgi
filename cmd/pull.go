package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"

	"github.com/spf13/cobra"
)

// pullCmd represents the pull command
var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Runs git pull for each service folder",
	Run:   runPull,
}

func init() {
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = utils.RunCombinedCmd(
		"git pull",
		utils.CorgiComposePathDir,
	)
	if err != nil {
		fmt.Println(err)
	}
	isRunOnce, err := cmd.Root().Flags().GetBool("runOnce")
	if err != nil {
		return
	}
	for _, service := range corgi.Services {
		corgiComposeExists, err := utils.CheckIfFileExistsInDirectory(
			service.AbsolutePath,
			utils.CorgiComposeDefaultName,
		)
		if err != nil {
			fmt.Println(err)
		}

		var pullCmdToExecute string
		if corgiComposeExists && !isRunOnce {
			pullCmdToExecute = "corgi pull --silent --runOnce"
		} else {
			pullCmdToExecute = "git pull"
		}

		err = utils.RunServiceCmd(
			service.ServiceName,
			pullCmdToExecute,
			service.AbsolutePath,
			true,
		)
		if err != nil {
			fmt.Println("pull failed for", service.ServiceName, "error:", err)
		}
	}
}
