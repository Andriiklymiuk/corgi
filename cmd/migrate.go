package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var migrateDryRun bool

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move a legacy corgi_services/ folder under .corgi/",
	Long: `Everything corgi generates now lives in one place: .corgi/corgi_services/.
Older checkouts keep a top-level corgi_services/; this moves it, and rewrites
the matching .gitignore lines. Any corgi command does it for you on the way
past — this is the version that says what it did.

Nothing moves while services are up. Stop them first: corgi stop`,
	Run: func(cmd *cobra.Command, _ []string) {
		utils.SkipCorgiServicesMigration = true
		if _, err := utils.GetCorgiServices(cmd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		dir := utils.CorgiComposePathDir
		legacy := filepath.Join(dir, utils.CorgiServicesName)
		target := filepath.Join(dir, utils.CorgiDirName, utils.CorgiServicesName)

		if _, err := os.Stat(legacy); err != nil {
			fmt.Printf("nothing to move — already at %s\n", target)
			return
		}
		if migrateDryRun {
			fmt.Printf("would move %s\n        to %s\n", legacy, target)
			return
		}
		moved, err := utils.MigrateCorgiServices(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		if !moved {
			fmt.Printf("nothing to move — already at %s\n", target)
			return
		}
		fmt.Printf("moved %s\n   to %s\n", legacy, target)
		fmt.Println("check .gitignore, then commit it")
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Print what would move and stop")
}
