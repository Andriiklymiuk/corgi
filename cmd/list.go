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

type listEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func toListEntries(paths []utils.CorgiExecPath) []listEntry {
	entries := make([]listEntry, 0, len(paths))
	for _, ep := range paths {
		entries = append(entries, listEntry{Name: ep.Name, Description: ep.Description, Path: ep.Path})
	}
	return entries
}

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
			utils.Infof("Error clearing executed paths: %s\n", err)
			return
		}
		utils.Info("All executed paths have been cleared.")
		return
	}

	useSpinner := !utils.CIMode && !utils.JSONOutput
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	if useSpinner {
		s.Start()
	}
	defer func() {
		if useSpinner {
			s.Stop()
		}
	}()

	paths, err := utils.ListExecPaths()
	if err != nil {
		utils.Infof("Error retrieving executed paths: %s\n", err)
		return
	}

	if useSpinner {
		s.Stop()
	}

	if utils.JSONOutput {
		utils.PrintJSON(toListEntries(paths))
		return
	}

	if len(paths) == 0 {
		utils.Info("No executed global corgi paths found.")
		return
	}

	fmt.Println("Globally executed corgi paths:")
	for _, ep := range paths {
		printExecPath(ep)
		fmt.Println()
	}
}

func printExecPath(ep utils.CorgiExecPath) {
	if ep.Name == "" && ep.Description == "" {
		fmt.Println(ep.Path)
		return
	}
	if ep.Name != "" {
		fmt.Printf("Name: %s%s%s\n", art.BlueColor, ep.Name, art.WhiteColor)
	}
	fmt.Printf("Path: %s\n", ep.Path)
	if ep.Description != "" {
		fmt.Printf("Description: %s\n", ep.Description)
	}
}
