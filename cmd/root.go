package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
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
	Version: "1.1.24",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() string {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	err := rootCmd.Execute()

	generateCobraDocs(rootCmd)

	if err != nil {
		os.Exit(1)
	}

	return rootCmd.CalledAs()
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.PersistentFlags().BoolP("doc", "", false, "Generate cobra docs")
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

	rootCmd.PersistentFlags().StringSliceVarP(
		&utils.ServicesItemsFromFlag,
		"services",
		"",
		[]string{},
		`Slice of services to choose from.

If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
none - will ignore all services run.
(--services app,server)

By default all services are included and run.
		`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&utils.DbServicesItemsFromFlag,
		"dbServices",
		"",
		[]string{},
		`Slice of db_services to choose from.

If you provide at least 1 db_service here, than corgi will choose only this db_service, while ignoring all others.
none - will ignore all db_services run.
(--dbServices db,db1,db2)

By default all db_services are included and run.
		`,
	)
}

func generateCobraDocs(cmd *cobra.Command) {
	shouldGenerateCobraDocs, err := cmd.Flags().GetBool("doc")
	if err != nil {
		fmt.Println("Couldn't read flag:", err)
	}

	if !shouldGenerateCobraDocs {
		return
	}
	err = doc.GenMarkdownTree(cmd, "./resources/readme")
	if err != nil {
		fmt.Println("Cobra docs are not regenerated: ", err)
	} else {
		fmt.Println("Cobra docs are generated, exiting ..")
	}
	os.Exit(1)

}
