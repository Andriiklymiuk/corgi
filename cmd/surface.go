package cmd

import (
	"fmt"

	"andriiklymiuk/corgi/utils"
	"github.com/spf13/cobra"
)

// The changed surface on the command line, for a person or a skill that has
// no MCP handy: the same list corgi_diff --surface returns.
var surfaceCmd = &cobra.Command{
	Use:   "surface",
	Short: "What a reviewer reads first: the public slice of the stack's diff",
	Long: `Lists the exported symbols, routes, contracts, migrations and config files the
current branches changed in every repository of the stack, against a base
branch, with removals and signature changes marked breaking. No toolchain
needed: it reads the diff. Paste the Markdown into the pull request body;
review it before the diff.

  corgi surface
  corgi surface --base develop --json
  corgi surface --branch feature/ABC-123/limits   # the branch's worktrees`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		base, _ := cmd.Flags().GetString("base")
		branch, _ := cmd.Flags().GetString("branch")
		composePath, _ := cmd.Flags().GetString("filename")
		out, err := mcpSurface(composePath, base, branch)
		if err != nil {
			exitWithError("surface", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(out)
			return
		}
		fmt.Print(out.(map[string]any)["markdown"])
	},
}

func init() {
	surfaceCmd.Flags().String("base", "", "base branch (default: main)")
	surfaceCmd.Flags().String("branch", "", "diff the existing worktrees of this branch instead of the checkouts")
	rootCmd.AddCommand(surfaceCmd)
}
