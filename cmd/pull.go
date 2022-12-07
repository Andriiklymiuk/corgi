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
	for _, service := range corgi.Services {
		err = utils.RunServiceCmd(service.ServiceName, "git pull", service.Path)
		if err != nil {
			fmt.Println("pull failed for", service.ServiceName, "error:", err)
		}
	}
}
