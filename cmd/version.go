package cmd

import (
	"fmt"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the installed corgi version",
	Example: `corgi version

corgi version --json`,
	Run: func(cmd *cobra.Command, _ []string) {
		if utils.JSONOutput {
			utils.PrintJSON(struct {
				Version   string `json:"version"`
				Changelog string `json:"changelog"`
			}{
				APP_VERSION,
				"https://github.com/Andriiklymiuk/corgi/releases/tag/v" + APP_VERSION,
			})
			return
		}
		fmt.Printf("corgi version %s\n", APP_VERSION)
		fmt.Printf("Changelog: https://github.com/Andriiklymiuk/corgi/releases/tag/v%s\n", APP_VERSION)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
