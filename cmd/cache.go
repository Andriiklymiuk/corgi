package cmd

import (
	"fmt"
	"os"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "What a CI cache should persist between runs",
}

var cachePathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Print the directories worth caching, and a key derived from every cacheKey file",
	Long: `Print the dependency directories a CI cache should persist, and a key that
changes whenever any beforeStart cacheKey file changes.

Derived from the compose file, so the list cannot drift when a service is added.
A service opts in by giving a beforeStart step a cacheKey; without one corgi
cannot skip that install anyway.

corgi_services/.cache is always included. It holds the markers that let corgi
skip an unchanged step, and restoring it without the dependency directories
would make corgi skip an install whose output is missing.

Examples:
  corgi cache paths
  corgi cache paths --json
  corgi cache paths --key`,
	Run: runCachePaths,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cachePathsCmd)
	cachePathsCmd.Flags().Bool("key", false, "Print only the cache key")
}

func runCachePaths(cmd *cobra.Command, _ []string) {
	// Set before the config is read, so the "using compose file" line does not
	// land in a command substitution.
	utils.PayloadOnStdout = true

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}

	plan := utils.CachePathsFor(corgi)

	if keyOnly, _ := cmd.Flags().GetBool("key"); keyOnly {
		fmt.Println(plan.Key)
		return
	}
	if utils.JSONOutput {
		utils.PrintJSON(plan)
		return
	}
	// Newline-separated, which is the format GitHub's cache action expects for
	// a multi-line path input.
	fmt.Println(strings.Join(plan.Paths, "\n"))
}
