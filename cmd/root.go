package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "corgi",
	Short: "Corgi cli magic friend",
	Long: `
This cli is created to make life easier.
The goal is to create smth flexible and robust.

WOOF 🐶
	`,
	Example: `
corgi init

corgi run
`,
	Version: "1.1.28",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() string {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	err := rootCmd.Execute()

	if err != nil {
		os.Exit(1)
	}

	return rootCmd.CalledAs()
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.PersistentFlags().BoolP(
		"silent",
		"",
		false,
		"Hide all welcome messages",
	)
	rootCmd.PersistentFlags().StringP(
		"filename",
		"f",
		"",
		"Custom filepath for for corgi-compose",
	)
	rootCmd.PersistentFlags().BoolP(
		"fromScratch",
		"",
		false,
		"Clean corgi_services folder before running",
	)
	rootCmd.PersistentFlags().BoolP(
		"describe",
		"",
		false,
		"Describe contents of corgi-compose file",
	)
}
