/*
Copyright © 2022 ANDRII KLYMIUK
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
corgi clean -i all
corgi clean -i db,corgi_services,services
corgi clean -i db
`,
	Run: runClean,
}

var cleanItems []string

func runClean(cobra *cobra.Command, args []string) {
	for _, itemToDelete := range cleanItems {
		switch itemToDelete {
		case "all":
			utils.ExecuteForEachService("down")
			utils.CleanCorgiServicesFolder()
			cleanServices(cobra)
			return
		case "db":
			utils.ExecuteForEachService("down")
		case "corgi_services":
			utils.CleanCorgiServicesFolder()
		case "services":
			cleanServices(cobra)
		}
	}
}

func cleanServices(cobra *cobra.Command) {
	corgi, err := utils.GetCorgiServices(cobra)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}
	for _, service := range corgi.Services {
		if service.Path == "" || service.Path == "." {
			return
		}

		err := os.RemoveAll(service.Path)
		if err != nil {
			fmt.Printf("couldn't delete %s folder: %s", service.Path, err)
			return
		}
		fmt.Printf("🗑️ cleaned up %s folder", service.Path)
	}
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

all - equal to writing db,corgi_services,services in items
		`,
	)
	cleanCmd.MarkFlagRequired("items")
}
