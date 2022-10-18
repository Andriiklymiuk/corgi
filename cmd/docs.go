/*
Copyright © 2022 ANDRII KLYMIUK
*/
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// docsCmd represents the docs command
var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Do stuff with docs",
	Long:  `Helper set of commands to make your life easier with docs and corgi `,
	Run:   runDocs,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.PersistentFlags().BoolP("generate", "g", false, "Generate cobra docs. Useful for development only, because it updates corgi docs.")
}

func runDocs(cmd *cobra.Command, args []string) {
	generateCobraDocs(cmd)
}

func generateCobraDocs(cmd *cobra.Command) {
	shouldGenerateCobraDocs, err := cmd.Flags().GetBool("generate")
	if err != nil {
		fmt.Println("Couldn't read flag:", err)
	}

	if !shouldGenerateCobraDocs {
		return
	}
	err = doc.GenMarkdownTree(cmd.Root(), "./resources/readme")
	if err != nil {
		fmt.Println("Cobra docs are not regenerated: ", err)
	} else {
		fmt.Println("Cobra docs are generated, exiting ..")
	}
	os.Exit(1)

}
