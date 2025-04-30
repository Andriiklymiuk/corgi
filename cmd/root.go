package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var APP_VERSION = "1.8.8"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "corgi",
	Short: "Corgi cli magic friend",
	Long: `
This cli is created to make life easier.
The goal is to create smth flexible and robust.

WOOF 🐶
	`,
	Example: `corgi init

corgi run`,
	Version: APP_VERSION,
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

	rootCmd.PersistentFlags().StringP(
		"dockerContext",
		"",
		"default",
		"Specify docker context to use, can be default,orbctl,colima",
	)
	rootCmd.PersistentFlags().StringP(
		"fromTemplate",
		"t",
		"",
		"Create corgi service from template url",
	)
	rootCmd.PersistentFlags().StringP(
		"fromTemplateName",
		"",
		"",
		"Create corgi service from template name and url",
	)
	rootCmd.PersistentFlags().BoolP(
		"exampleList",
		"l",
		false,
		"List examples to choose from. Click on any example to download it",
	)
	rootCmd.PersistentFlags().StringP(
		"privateToken",
		"",
		"",
		"Private token for private repositories to download files",
	)
	rootCmd.PersistentFlags().BoolP(
		"runOnce",
		"o",
		false,
		"Run corgi once and exit",
	)
	rootCmd.PersistentFlags().BoolP(
		"global",
		"g",
		false,
		"Use global path to one of the services",
	)
	rootCmd.SetVersionTemplate("corgi version {{.Version}}\nChangelog: https://github.com/Andriiklymiuk/corgi/releases/tag/v{{.Version}}\n")
}
