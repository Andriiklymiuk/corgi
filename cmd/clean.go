/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Cleans all services",
	Long: `Cleans all db, corgi_services folder, cloned repos, etc.
Useful to clean start corgi as new.
Similar to --fromScratch flag used in other commands, but this is more generic.

Requires items flag.
`,
	Example: `
corgi clean -i db,corgi_services,services
corgi clean -i db
`,
	Run: runClean,
}

var cleanItems []string

func runClean(cobra *cobra.Command, args []string) {
	for _, itemToDelete := range cleanItems {
		switch itemToDelete {
		case "db":
			utils.ExecuteForEachService("down")
		case "corgi_services":
			utils.CleanCorgiServicesFolder()
		case "services":
			corgi, err := utils.GetCorgiServices(cobra)
			if err != nil {
				fmt.Printf("couldn't get services config, error: %s\n", err)
				continue
			}
			for _, service := range corgi.Services {
				cleanServiceFolder(service.Path)
			}
		}
	}
}

func cleanServiceFolder(path string) {
	if path == "" || path == "." {
		return
	}

	err := os.RemoveAll(path)
	if err != nil {
		fmt.Printf("couldn't delete %s folder: %s", path, err)
		return
	}
	fmt.Printf("🗑️ cleaned up %s folder", path)
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().StringSliceVarP(
		&cleanItems,
		"items",
		"i",
		[]string{},
		`Slice of items to clean, like: db,corgi_services,services. 
		
db - down all databases, that were added to corgi_services folder.
corgi_services - clean corgi_services folder.
services - delete all services folders (useful, when you want to clean cloned repos folders)
		`,
	)
	cleanCmd.MarkFlagRequired("items")
}
