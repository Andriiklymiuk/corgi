package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

var cleanList bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all executed corgi-compose paths",
	Long:  `This command lists all the paths to corgi-compose files that have been executed.`,
	Run:   listRun,
}

func init() {
	listCmd.Flags().BoolVar(&cleanList, "cleanList", false, "Clear all the listed paths")
	rootCmd.AddCommand(listCmd)
}

func listRun(cmd *cobra.Command, args []string) {
	if cleanList {
		if err := utils.ClearExecPaths(); err != nil {
			fmt.Printf("Error clearing executed paths: %s\n", err)
			return
		}
		fmt.Println("All executed paths have been cleared.")
		return
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Start()
	defer s.Stop()

	paths, err := utils.ListExecPaths()
	if err != nil {
		fmt.Printf("Error retrieving executed paths: %s\n", err)
		return
	}

	s.Stop()

	if len(paths) == 0 {
		fmt.Println("No executed global corgi paths found.")
		return
	}

	fmt.Println("Globally executed corgi paths:")
	for _, ep := range paths {
		if ep.Name != "" || ep.Description != "" {
			if ep.Name != "" {
				var name = art.BlueColor + ep.Name + art.WhiteColor
				fmt.Printf("Name: %s\n", name)
			}
			fmt.Printf("Path: %s\n", ep.Path)
			if ep.Description != "" {
				fmt.Printf("Description: %s\n", ep.Description)
			}
		} else {
			fmt.Println(ep.Path)
		}
		fmt.Println()
	}
}
