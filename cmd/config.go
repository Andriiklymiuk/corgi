package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show global corgi preferences stored in ~/.corgi/config.yml",
	Long: `Show global corgi user preferences.

The preferences live at ~/.corgi/config.yml and persist across projects.
To toggle notifications: corgi notifications on|off|test.
To reset everything: delete ~/.corgi/config.yml.`,
	Args: cobra.NoArgs,
	Run:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the absolute path to the user config file",
	Run: func(cmd *cobra.Command, _ []string) {
		dir, err := utils.GetUserConfigDir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println(filepath.Join(dir, "config.yml"))
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configPathCmd)
}

func runConfigShow(cmd *cobra.Command, _ []string) {
	cfg, err := utils.LoadUserConfig()
	if err != nil {
		fmt.Printf("%s❌ Failed to read user config: %v%s\n", art.RedColor, err, art.WhiteColor)
		os.Exit(1)
	}
	dir, _ := utils.GetUserConfigDir()
	path := filepath.Join(dir, "config.yml")

	state := "off"
	if cfg.Notifications {
		state = "on"
	}
	fmt.Printf("%s📁 %s%s\n", art.CyanColor, path, art.WhiteColor)
	fmt.Printf("  schema version: %d\n", cfg.Version)
	fmt.Printf("  notifications:  %s\n", state)
	fmt.Println()
	fmt.Println("Toggle notifications with: corgi notifications on|off|test")
}
